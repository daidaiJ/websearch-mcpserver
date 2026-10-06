package provider

import (
	"websearch/pkg/search/core"
	"fmt"
	"net/url"
	"strings"
	"time"
	"websearch/pkg/antirobot"
	"websearch/pkg/client"
)

const anysearchAPIEndpoint = "https://api.anysearch.com/v1/search"

// AnysearchSearchImpl 实现 core.SearchInf 接口，通过 AnySearch 搜索 API 搜索（https://www.anysearch.com/docs）。
// 响应为统一 envelope：code=0 成功，code=-1 失败（可能携带 error_code）；
// Key 无效/禁用/过期时网关返回 401/403，不会静默降级为匿名模式。
// 零 Key 匿名档（P0-2）：keys 为 nil 时省略 Authorization 头——上游明确以
// "无鉴权头"选择匿名 IP 限流档（上游 anysearch.py 注释；2026-10-06 本项目出口实测可用），
// 此时请求受保守固定限流钳制，失败不包装 KeyError（无 Key 可归责/失效）。
type AnysearchSearchImpl struct {
	name           string
	keys           *KeyPool              // nil = 匿名档
	anonLimiter    *antirobot.RateLimiter // 仅匿名档启用（上游未公布 IP 档阈值，取保守固定值）
	numResults     int
	excludeDomains []string
	endpoint       string // API 端点，默认 anysearchAPIEndpoint（测试可覆盖）
}

type anysearchSearchReq struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

type anysearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Content string `json:"content"`
}

type anysearchSearchResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Results []anysearchResult `json:"results"`
	} `json:"data"`
}

// NewAnysearchSearch 创建 AnySearch 搜索实例，支持 KeyPool 轮询。
// keys 为 nil 时启用零 Key 匿名档（省略 Authorization 头 + 保守限流钳制）。
// numResults <= 0 时使用默认 10。
func NewAnysearchSearch(keys *KeyPool, numResults int, excludeDomains []string) *AnysearchSearchImpl {
	if numResults <= 0 {
		numResults = 10
	}
	a := &AnysearchSearchImpl{
		name:           "anysearch",
		keys:           keys,
		numResults:     numResults,
		excludeDomains: excludeDomains,
		endpoint:       anysearchAPIEndpoint,
	}
	if keys == nil {
		a.anonLimiter = antirobot.NewRateLimiter(1, 20).WithMinInterval(time.Second)
	}
	return a
}

func (a *AnysearchSearchImpl) Name() string { return a.name }

func (a *AnysearchSearchImpl) Search(query string) (string, error) {
	results, err := a.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return a.MergeContent(query, results)
}

func (a *AnysearchSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	req := anysearchSearchReq{
		Query:      query,
		MaxResults: a.numResults,
	}
	var resp anysearchSearchResp
	call := client.DefaultClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp)

	// 认证头按 KeyPool 有无分流：有 Key 走轮转并包装 KeyError（精确冷却）；
	// 匿名档省略 Authorization 头（带空 Bearer 会被上游拒绝），限流钳制前置
	var key string
	if a.keys != nil {
		key = a.keys.Next()
		call = call.SetHeader("Authorization", fmt.Sprintf("Bearer %s", key))
	} else if !a.anonLimiter.Allow() {
		perSec, perMin := a.anonLimiter.Limits()
		return nil, fmt.Errorf("anysearch 匿名档触发本端限流（%d/s、%d/min 保守钳制，匿名档按出口 IP 限流）", perSec, perMin)
	}

	res, err := call.Post(a.endpoint)
	if err != nil {
		if a.keys == nil {
			return nil, fmt.Errorf("anysearch 搜索 API 调用失败: %w", err)
		}
		return nil, &KeyError{Key: key, Err: fmt.Errorf("anysearch 搜索 API 调用失败: %w", err)}
	}
	if res.StatusCode() != 200 {
		if a.keys == nil {
			return nil, fmt.Errorf("anysearch 搜索 API 返回错误状态码: %d", res.StatusCode())
		}
		return nil, &KeyError{Key: key, Err: fmt.Errorf("anysearch 搜索 API 返回错误状态码: %d", res.StatusCode())}
	}
	if resp.Code != 0 {
		if a.keys == nil {
			return nil, fmt.Errorf("anysearch 搜索 API 返回错误: code=%d message=%s", resp.Code, resp.Message)
		}
		return nil, &KeyError{Key: key, Err: fmt.Errorf("anysearch 搜索 API 返回错误: code=%d message=%s", resp.Code, resp.Message)}
	}
	if len(resp.Data.Results) == 0 {
		return nil, fmt.Errorf("anysearch 搜索 API 结果为空")
	}

	ret := make([]core.SearchResult, 0, len(resp.Data.Results))
	for _, r := range resp.Data.Results {
		if isBlockedHost(r.URL, a.excludeDomains) {
			continue
		}
		content := r.Content
		if content == "" {
			content = r.Snippet
		}
		ret = append(ret, core.SearchResult{
			Title:   r.Title,
			Url:     strings.TrimSpace(r.URL),
			Content: content,
			Engine:  a.name,
		})
	}
	return ret, nil
}

// isBlockedHost 本地黑名单过滤：AnySearch API 不支持 exclude_domains 参数，
// 与 bing 引擎同语义 —— host 精确匹配或子域后缀匹配（大小写不敏感）。
func isBlockedHost(rawURL string, blocked []string) bool {
	if len(blocked) == 0 || rawURL == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(rawURL))
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
		host = strings.ToLower(parsed.Hostname())
	}
	for _, d := range blocked {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func (a *AnysearchSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
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
