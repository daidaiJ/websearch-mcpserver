package googlenews

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websearch/pkg/antirobot"
)

// 发布方 URL 解析（对齐上游 free-search-mcp gnews.py，2026-10-06 本项目出口实测可回源）：
// news.google.com/rss/articles/CBM… 跳转 blob 是不透明 protobuf，HTTP 直跳与
// 无头渲染都落在空 JS 壳上。Google 自家客户端的做法：从文章壳页读取签名
// （data-n-a-sg）+ 时间戳（data-n-a-ts）+ 文章 id（data-n-a-id），POST 到
// batchexecute 端点换回发布方 URL。这里原样重放该交互。
// 解析有界三重约束：并发数、总预算、连续失败上限；失败保留跳转链接（fetch 仍可跟随），
// 成功结果进程内记忆，避免重复支付 ~600KB 的壳页下载。

const (
	resolveConcurrency = 4                // 同时解析的链接数
	resolveBudget      = 10 * time.Second // 一批解析的总预算（壳页 ~600KB 经代理下载慢，实测校准）
	resolveMaxFailures = 3                // 连续失败上限（判定 RPC 不可用，剩余放弃）
	resolveMemoMax     = 2048             // 进程内记忆上限（FIFO 淘汰）
)

// 单测可指向 httptest 服务。
var (
	batchexecuteURL = "https://news.google.com/_/DotsSplashUi/data/batchexecute"
	shellPagePrefix = "https://news.google.com"
)

var (
	sgRe      = regexp.MustCompile(`data-n-a-sg="([^"]+)"`)
	tsRe      = regexp.MustCompile(`data-n-a-ts="([^"]+)"`)
	idRe      = regexp.MustCompile(`data-n-a-id="([^"]+)"`)
	garturlRe = regexp.MustCompile(`\\"garturlres\\",\\"(https?[^\\"]+)\\"`)
)

// 解析结果进程内记忆（GN 文章链接是一次性读取，简单 FIFO 淘汰即可）。
var (
	memoMu   sync.Mutex
	memo     = make(map[string]string)
	memoKeys []string // 插入顺序，FIFO 淘汰用
)

func memoGet(key string) string {
	memoMu.Lock()
	defer memoMu.Unlock()
	return memo[key]
}

func memoPut(key, val string) {
	memoMu.Lock()
	defer memoMu.Unlock()
	if _, exists := memo[key]; !exists {
		memoKeys = append(memoKeys, key)
	}
	memo[key] = val
	for len(memoKeys) > resolveMemoMax {
		oldest := memoKeys[0]
		memoKeys = memoKeys[1:]
		delete(memo, oldest)
	}
}

// isGoogleNewsURL 判断是否为可解析的 Google News 文章跳转 blob
// （/rss/articles/CBM…、/articles/CBM…、/read/CBM…）。
func isGoogleNewsURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "news.google.com" && host != "www.news.google.com" {
		return false
	}
	return strings.Contains(u.Path, "/articles/") || strings.Contains(u.Path, "/read/")
}

// resolvePublisherURLs 批量把跳转 blob 原位改写为发布方 URL（尽力而为）：
// 只解析前 numResults 条（超出部分会被 max_size 截断，每条解析需拖一次
// ~600KB 壳页）；预算内解析多少算多少，失败保留原链接。
func (e *gnewsEngine) resolvePublisherURLs(results []antirobot.Result) {
	limit := e.num
	if limit <= 0 {
		limit = 10
	}
	if limit > len(results) {
		limit = len(results)
	}
	var targets []*antirobot.Result
	for i := 0; i < limit; i++ {
		if isGoogleNewsURL(results[i].URL) {
			targets = append(targets, &results[i])
		}
	}
	if len(targets) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveBudget)
	defer cancel()
	gate := make(chan struct{}, resolveConcurrency)
	var failures atomic.Int32
	var wg sync.WaitGroup
	for _, t := range targets {
		if failures.Load() >= resolveMaxFailures || ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(t *antirobot.Result) {
			defer wg.Done()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				return
			}
			if resolved := resolveGoogleNewsURL(ctx, e.client, t.URL); resolved != "" {
				failures.Store(0)
				t.URL = resolved
			} else {
				failures.Add(1)
			}
		}(t)
	}
	wg.Wait()
}

// resolveGoogleNewsURL 解析单个跳转 blob 为发布方 URL；任何失败返回空串
// （调用方保留原跳转链接，无回退损失）。
func resolveGoogleNewsURL(ctx context.Context, client *http.Client, articleURL string) string {
	if !isGoogleNewsURL(articleURL) {
		return ""
	}
	if hit := memoGet(articleURL); hit != "" {
		return hit
	}

	sg, ts, id, err := fetchShellAttrs(ctx, client, articleURL)
	if err != nil {
		return ""
	}
	resolved, err := postBatchExecute(ctx, client, sg, ts, id)
	if err != nil || resolved == "" {
		return ""
	}
	memoPut(articleURL, resolved)
	return resolved
}

// fetchShellAttrs 下载文章壳页并提取 data-n-a-* 属性。
func fetchShellAttrs(ctx context.Context, client *http.Client, articleURL string) (sg, ts, id string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL, nil)
	if err != nil {
		return "", "", "", err
	}
	setGNewsHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", "", fmt.Errorf("shell page HTTP %d", resp.StatusCode)
	}
	html := readBounded(resp.Body)

	sgM := sgRe.FindStringSubmatch(html)
	tsM := tsRe.FindStringSubmatch(html)
	idM := idRe.FindStringSubmatch(html)
	if sgM == nil || tsM == nil || idM == nil {
		return "", "", "", fmt.Errorf("shell page 缺少 data-n-a-* 属性（页面结构可能已变化）")
	}
	return sgM[1], tsM[1], idM[1], nil
}

// postBatchExecute 重放 Google 客户端的 garturlreq RPC 换取发布方 URL。
// 注意 ts 必须为数字形态（上游 int(ts)），字符串形态服务端返回 400。
func postBatchExecute(ctx context.Context, client *http.Client, sg, ts, articleID string) (string, error) {
	tsNum, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", fmt.Errorf("ts 非数字: %q", ts)
	}
	inner, err := json.Marshal([]any{
		"garturlreq",
		[]any{
			[]any{"X", "X", []any{"X", "X"}, nil, nil, 1, 1, "US:en", nil, 1, nil, nil, nil, nil, nil, 0, 1},
			"X", "X", 1, []any{1, 1, 1}, 1, 1, nil, 0, 0, nil, 0,
		},
		articleID,
		tsNum,
		sg,
	})
	if err != nil {
		return "", err
	}
	outer, err := json.Marshal([]any{[]any{[]any{"Fbv4je", string(inner), nil, "generic"}}})
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("f.req", string(outer))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, batchexecuteURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	setGNewsHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("batchexecute HTTP %d", resp.StatusCode)
	}
	m := garturlRe.FindStringSubmatch(readBounded(resp.Body))
	if m == nil {
		return "", fmt.Errorf("batchexecute 响应中无 garturlres")
	}
	return m[1], nil
}

func setGNewsHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
}

// readBounded 读取响应体（上限 2MB，壳页典型 ~600KB，防异常大响应）。
func readBounded(r io.Reader) string {
	body, err := io.ReadAll(io.LimitReader(r, 2*1024*1024))
	if err != nil {
		return ""
	}
	return string(body)
}
