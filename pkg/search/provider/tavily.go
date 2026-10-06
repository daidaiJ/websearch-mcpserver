package provider

import (
	"websearch/pkg/search/core"
	"fmt"
	"strings"
	"websearch/pkg/client"
)

// TavilySearchImpl 实现 core.SearchInf 接口，通过 Tavily Search API 搜索。
type TavilySearchImpl struct {
	name              string
	keys              *KeyPool
	timeRange         string // "day", "week", "month", "year"，空表示不限
	includeDomains    []string
	excludeDomains    []string
	includeRawContent bool // true=请求 include_raw_content，让 API 返回页面原文
}

type tavilySearchReq struct {
	Query             string   `json:"query"`
	SearchDepth       string   `json:"search_depth"`
	TimeRange         string   `json:"time_range,omitempty"`
	StartDate         string   `json:"start_date,omitempty"`
	EndDate           string   `json:"end_date,omitempty"`
	IncludeDomains    []string `json:"include_domains,omitempty"`
	ExcludeDomains    []string `json:"exclude_domains,omitempty"`
	IncludeRawContent bool     `json:"include_raw_content,omitempty"`
}

type tavilyResult struct {
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Content    string  `json:"content"`
	RawContent string  `json:"raw_content,omitempty"`
	Score      float64 `json:"score"`
}

type tavilySearchResp struct {
	Results []tavilyResult `json:"results"`
}

// TavilyOption 可选参数。
type TavilyOption func(*TavilySearchImpl)

// WithRawContent 请求 API 返回页面原文（raw_content），默认关闭（仅摘要片段）。
func WithRawContent(on bool) TavilyOption {
	return func(t *TavilySearchImpl) { t.includeRawContent = on }
}

// NewTavilySearch 创建 Tavily 搜索实例，支持 KeyPool 轮询。
func NewTavilySearch(keys *KeyPool, excludeDomains []string, opts ...TavilyOption) *TavilySearchImpl {
	return newTavilySearch(keys, nil, excludeDomains, opts...)
}

// NewTavilySearchWithDomains 创建支持限定域名的 Tavily 搜索实例。
func NewTavilySearchWithDomains(keys *KeyPool, includeDomains, excludeDomains []string, opts ...TavilyOption) *TavilySearchImpl {
	return newTavilySearch(keys, includeDomains, excludeDomains, opts...)
}

func newTavilySearch(keys *KeyPool, includeDomains, excludeDomains []string, opts ...TavilyOption) *TavilySearchImpl {
	t := &TavilySearchImpl{
		name:           "tavily_api",
		keys:           keys,
		includeDomains: includeDomains,
		excludeDomains: excludeDomains,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *TavilySearchImpl) Name() string { return t.name }

func (t *TavilySearchImpl) Search(query string) (string, error) {
	results, err := t.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return t.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口，支持动态时间范围。
func (t *TavilySearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	timeRange := lookbackDaysToTavilyRange(lookbackDays)
	saved := t.timeRange
	t.timeRange = timeRange
	defer func() { t.timeRange = saved }()
	return t.SearchRaw(query)
}

// lookbackDaysToTavilyRange 将天数转换为 Tavily API 的 time_range 值。
func lookbackDaysToTavilyRange(days int) string {
	switch {
	case days <= 0:
		return ""
	case days <= 1:
		return "day"
	case days <= 7:
		return "week"
	case days <= 30:
		return "month"
	default:
		return "year"
	}
}

func (t *TavilySearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	req := tavilySearchReq{
		Query:             query,
		SearchDepth:       "basic",
		TimeRange:         t.timeRange,
		IncludeDomains:    t.includeDomains,
		ExcludeDomains:    t.excludeDomains,
		IncludeRawContent: t.includeRawContent,
	}
	var resp tavilySearchResp
	key := t.keys.Next()
	res, err := client.DefaultClient.R().
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", key)).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post("https://api.tavily.com/search")
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("tavily 搜索api 调用失败，%w", err)}
	}
	if res.StatusCode() != 200 {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("tavily 搜索api 返回错误状态码: %d", res.StatusCode())}
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("tavily 搜索api 内容为空")
	}
	ret := make([]core.SearchResult, 0, len(resp.Results))
	for _, val := range resp.Results {
		ret = append(ret, core.SearchResult{
			Title:   val.Title,
			Url:     strings.TrimSpace(val.URL),
			Content: firstNonEmpty(val.RawContent, val.Content),
			Score:   val.Score,
			Engine:  t.name,
		})
	}
	return ret, nil
}

func (t *TavilySearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {

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
