package provider

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"websearch/pkg/client"
	"websearch/pkg/search/core"
)

// keenableAppTitle 是 public 免 Key 端点必需的应用标识头（X-Keenable-Title）。
const keenableAppTitle = "websearch-mcpserver"

// KeenableSearchImpl 通过 Keenable Search/Fetch API 搜索与抓取。
// keyed 端点按 credits 计费；KeyPool 为空时自动走 public 免 Key 端点
// （X-Keenable-Title 标识应用名，按出口 IP 限流，不消耗 credits）。
// 限流 10 请求/秒。fetch 仅能抓已索引 URL，live=true 时从源站实抓。
type KeenableSearchImpl struct {
	name string
	keys *KeyPool // nil = public 免 Key 档
	live bool
	// baseURL 可注入（单测覆写）；构造器落默认常量。
	baseURL string
	limit   int // 单次搜索结果上限（1-50，默认 10）
}

const keenableBaseURL = "https://api.keenable.ai/v1"

type keenableSearchReq struct {
	Query      string `json:"query"`
	Mode       string `json:"mode,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

type keenableResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Snippet     string `json:"snippet"`
	PublishedAt string `json:"published_at"`
	AcquiredAt  string `json:"acquired_at"`
}

type keenableSearchResp struct {
	Query   string           `json:"query"`
	Mode    string           `json:"mode"`
	Results []keenableResult `json:"results"`
}

type keenableFetchResp struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Author      string `json:"author"`
	PublishedAt int64  `json:"published_at"` // Unix 秒，可能为 0
	Content     string `json:"content"`
}

// NewKeenableSearch 创建 Keenable 实例；keys 为 nil 时走 public 免 Key 档。
func NewKeenableSearch(keys *KeyPool, live bool, limit int) *KeenableSearchImpl {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	return &KeenableSearchImpl{name: "keenable_api", keys: keys, live: live, baseURL: keenableBaseURL, limit: limit}
}

func (k *KeenableSearchImpl) Name() string { return k.name }

// usePublic 返回本次是否应走 public 端点：未配置 Key 池即 public。
func (k *KeenableSearchImpl) usePublic() bool { return k.keys == nil }

func (k *KeenableSearchImpl) Search(query string) (string, error) {
	results, err := k.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return k.MergeContent(query, results)
}

// SearchRawContext 实现 core.ContextSearcher。
func (k *KeenableSearchImpl) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	return k.search(ctx, query)
}

func (k *KeenableSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	return k.search(context.Background(), query)
}

func (k *KeenableSearchImpl) search(ctx context.Context, query string) ([]core.SearchResult, error) {
	endpoint := k.baseURL + "/search"
	req := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(keenableSearchReq{Query: query, Mode: "pro", MaxResults: k.limit})
	var key string
	if k.usePublic() {
		endpoint += "/public"
		req.SetHeader("X-Keenable-Title", keenableAppTitle)
	} else {
		key = k.keys.Next()
		req.SetHeader("X-API-Key", key)
	}
	var resp keenableSearchResp
	res, err := req.SetResult(&resp).Post(endpoint)
	if err != nil {
		if k.usePublic() {
			return nil, fmt.Errorf("keenable 搜索api 调用失败: %w", err)
		}
		return nil, &KeyError{Key: key, Err: fmt.Errorf("keenable 搜索api 调用失败: %w", err)}
	}
	if res.StatusCode() != http.StatusOK {
		err := fmt.Errorf("keenable 搜索api 返回错误状态码: %d", res.StatusCode())
		if !k.usePublic() && res.StatusCode() == http.StatusUnauthorized {
			return nil, &KeyError{Key: key, Err: err}
		}
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("keenable 搜索api 内容为空")
	}
	ret := make([]core.SearchResult, 0, len(resp.Results))
	for _, val := range resp.Results {
		item := core.SearchResult{
			Title:   strings.TrimSpace(val.Title),
			Url:     strings.TrimSpace(val.URL),
			Content: strings.TrimSpace(firstNonEmpty(val.Snippet, val.Description)),
			Engine:  k.name,
		}
		if d := strings.TrimSpace(val.PublishedAt); d != "" {
			item.PublishDate = d
			item.DateSource = core.DateSourceStructured
		}
		ret = append(ret, item)
	}
	return ret, nil
}

// Fetch 通过 Keenable Fetch API 抓取单页正文（markdown）。
// keyed 端点按 credits 计费；public 端点按 IP 限流、不消耗 credits。
// 仅能抓已索引 URL；live=true 时从源站实抓并可抓未索引页。
func (k *KeenableSearchImpl) Fetch(ctx context.Context, rawURL string, maxChars int) (string, error) {
	endpoint := k.baseURL + "/fetch"
	req := client.DefaultClient.R().
		SetContext(ctx).
		SetQueryParam("url", rawURL)
	if k.live {
		req.SetQueryParam("live", "true")
	}
	if maxChars > 0 {
		req.SetQueryParam("max_chars", strconv.Itoa(maxChars))
	}
	if k.usePublic() {
		endpoint += "/public"
		req.SetHeader("X-Keenable-Title", keenableAppTitle)
	} else {
		req.SetHeader("X-API-Key", k.keys.Next())
	}
	var resp keenableFetchResp
	res, err := req.SetResult(&resp).Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("keenable fetch api 调用失败: %w", err)
	}
	if res.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("keenable fetch api 返回错误状态码: %d", res.StatusCode())
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return "", fmt.Errorf("keenable fetch api 正文为空")
	}
	if title := strings.TrimSpace(resp.Title); title != "" && !strings.HasPrefix(text, "# ") {
		text = "# " + title + "\n\n" + text
	}
	return text, nil
}

func (k *KeenableSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if core.ShowMeta {
			buf.WriteString(core.FormatMDScore(i+1, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), core.FormatDateSource(val.PublishDate, val.DateSource), val.Content))
		} else {
			buf.WriteString(core.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}

var (
	_ core.SearchInf       = (*KeenableSearchImpl)(nil)
	_ core.ContextSearcher = (*KeenableSearchImpl)(nil)
	_ interface {
		Fetch(ctx context.Context, rawURL string, maxChars int) (string, error)
	} = (*KeenableSearchImpl)(nil)
)
