package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"websearch/pkg/i18n"
	"websearch/pkg/search/core"

	"websearch/pkg/client"
)

const exaAPIEndpoint = "https://api.exa.ai/search"

// ExaSearchImpl 实现 core.SearchInf 接口，通过 Exa Web Search API 搜索。
type ExaSearchImpl struct {
	name              string
	keys              *KeyPool
	numResults        int
	lookbackDays      int // 搜索时间范围（天），默认 90
	excludeDomains    []string
	includeText       bool // true=请求 contents.text，让 API 返回页面正文
	textMaxCharacters int  // 正文最大字符数（默认 3000）
}

type exaSearchReq struct {
	Query              string      `json:"query"`
	NumResults         int         `json:"numResults,omitempty"`
	StartPublishedDate string      `json:"startPublishedDate,omitempty"`
	EndPublishedDate   string      `json:"endPublishedDate,omitempty"`
	ExcludeDomains     []string    `json:"excludeDomains,omitempty"`
	Type               string      `json:"type,omitempty"`
	Contents           exaContents `json:"contents"`
}

type exaContents struct {
	Highlights bool     `json:"highlights"`
	Text       *exaText `json:"text,omitempty"`
}

type exaText struct {
	MaxCharacters int `json:"maxCharacters,omitempty"`
}

type exaResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	PublishedDate string   `json:"publishedDate,omitempty"`
	Highlights    []string `json:"highlights,omitempty"`
	Text          string   `json:"text,omitempty"`
}

type exaSearchResp struct {
	Results []exaResult `json:"results"`
}

// ExaOption 可选参数。
type ExaOption func(*ExaSearchImpl)

// WithTextContents 请求 API 返回页面正文（contents.text），默认关闭（仅 highlights）。
// maxCharacters <= 0 时取默认 3000。
func WithTextContents(maxCharacters int) ExaOption {
	if maxCharacters <= 0 {
		maxCharacters = 3000
	}
	return func(e *ExaSearchImpl) {
		e.includeText = true
		e.textMaxCharacters = maxCharacters
	}
}

// NewExaSearch 创建 Exa 搜索实例，默认搜索最近 90 天。
func NewExaSearch(keys *KeyPool, excludeDomains []string, opts ...ExaOption) *ExaSearchImpl {
	return newExaSearch(keys, 5, 90, excludeDomains, opts...)
}

// NewExaSearchWithResults 创建指定配置的 Exa 搜索实例。
// lookbackDays 控制搜索时间范围（天），<=0 时使用默认 90 天。
func NewExaSearchWithResults(keys *KeyPool, numResults, lookbackDays int, excludeDomains []string, opts ...ExaOption) *ExaSearchImpl {
	return newExaSearch(keys, numResults, lookbackDays, excludeDomains, opts...)
}

func newExaSearch(keys *KeyPool, numResults, lookbackDays int, excludeDomains []string, opts ...ExaOption) *ExaSearchImpl {
	if numResults <= 0 {
		numResults = 5
	}
	if lookbackDays <= 0 {
		lookbackDays = 90
	}
	e := &ExaSearchImpl{
		name:           "exa",
		keys:           keys,
		numResults:     numResults,
		lookbackDays:   lookbackDays,
		excludeDomains: excludeDomains,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// textContent 返回 contents.text 的请求体；未启用正文时返回 nil。
func (e *ExaSearchImpl) textContent() *exaText {
	if !e.includeText {
		return nil
	}
	if e.textMaxCharacters <= 0 {
		return &exaText{MaxCharacters: 3000}
	}
	return &exaText{MaxCharacters: e.textMaxCharacters}
}

func (e *ExaSearchImpl) Name() string { return e.name }

func (e *ExaSearchImpl) Search(query string) (string, error) {
	results, err := e.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return e.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口，支持动态时间范围。
func (e *ExaSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	request := *e
	request.lookbackDays = lookbackDays
	return request.SearchRaw(query)
}

// SearchRawContext 实现 core.ContextSearcher：ctx 下传 HTTP 请求。
func (e *ExaSearchImpl) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	return e.search(ctx, query)
}

// SearchRawWithTimeRangeContext 实现 core.ContextTimeRanger。
func (e *ExaSearchImpl) SearchRawWithTimeRangeContext(ctx context.Context, query string, lookbackDays int) ([]core.SearchResult, error) {
	request := *e
	request.lookbackDays = lookbackDays
	return request.search(ctx, query)
}

func (e *ExaSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	return e.search(context.Background(), query)
}

func (e *ExaSearchImpl) search(ctx context.Context, query string) ([]core.SearchResult, error) {
	req := exaSearchReq{
		Query:          query,
		NumResults:     e.numResults,
		ExcludeDomains: e.excludeDomains,
		Type:           "auto",
		Contents:       exaContents{Highlights: true, Text: e.textContent()},
	}
	if e.lookbackDays > 0 {
		now := time.Now().UTC()
		req.StartPublishedDate = now.AddDate(0, 0, -e.lookbackDays).Format(time.RFC3339)
		req.EndPublishedDate = now.Format(time.RFC3339)
	}

	var resp exaSearchResp
	key := e.keys.Next()
	res, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("x-api-key", key).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post(exaAPIEndpoint)
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("%s: %w", i18n.T("provider.exa.call_failed"), err)}
	}
	if res.StatusCode() != 200 {
		return nil, &KeyError{Key: key, Err: errors.New(i18n.T("provider.exa.bad_status", "Status", res.StatusCode()))}
	}
	if len(resp.Results) == 0 {
		return nil, errors.New(i18n.T("provider.exa.empty_results"))
	}

	ret := make([]core.SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		content := strings.TrimSpace(r.Text)
		if content == "" {
			content = strings.Join(r.Highlights, "\n")
		}
		ret = append(ret, core.SearchResult{
			Title:       r.Title,
			Url:         strings.TrimSpace(r.URL),
			Content:     content,
			PublishDate: r.PublishedDate,
			DateSource:  core.DateSourceStructured,
			Engine:      e.name,
		})
	}
	return ret, nil
}

func (e *ExaSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", errors.New(i18n.T("provider.merge.no_results"))
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
