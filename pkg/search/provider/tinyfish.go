package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"websearch/pkg/client"
	"websearch/pkg/i18n"
	"websearch/pkg/search/core"
)

// TinyFishSearchImpl 通过 TinyFish Search API 搜索（GET + X-API-Key）。
// 免费 12000 次/天（00:00 UTC 重置），默认限流 30 请求/分钟；仅 Key 档，无匿名档。
// 同时实现 Fetch（TinyFish Fetch API，POST 批量端点单 URL 使用），
// 供 cleanfetch 远端抓取链复用；fetch 免费额度按成功抓取的 URL 计数（1000 URL/天）。
type TinyFishSearchImpl struct {
	name string
	keys *KeyPool
	// 端点可注入（单测覆写）；构造器落默认常量。
	searchURL string
	fetchURL  string
}

const (
	tinyfishSearchEndpoint = "https://api.search.tinyfish.ai"
	tinyfishFetchEndpoint  = "https://api.fetch.tinyfish.ai"
)

type tinyfishResult struct {
	Position int    `json:"position"`
	SiteName string `json:"site_name"`
	Title    string `json:"title"`
	Snippet  string `json:"snippet"`
	URL      string `json:"url"`
	Date     string `json:"date"`
	PubYear  int    `json:"year"`
}

type tinyfishSearchResp struct {
	Query        string           `json:"query"`
	Results      []tinyfishResult `json:"results"`
	TotalResults int              `json:"total_results"`
}

type tinyfishFetchReq struct {
	URLs   []string `json:"urls"`
	Format string   `json:"format,omitempty"`
}

type tinyfishFetchResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	PublishedAt string `json:"published_date"`
}

type tinyfishFetchResp struct {
	Results []tinyfishFetchResult `json:"results"`
	Errors  []struct {
		URL    string `json:"url"`
		Error  string `json:"error"`
		Status int    `json:"status"`
	} `json:"errors"`
}

// NewTinyFishSearch 创建 TinyFish 搜索实例；keys 为 nil 时返回 nil（无匿名档，未配 Key 不启用）。
func NewTinyFishSearch(keys *KeyPool) *TinyFishSearchImpl {
	if keys == nil {
		return nil
	}
	return &TinyFishSearchImpl{name: "tinyfish_api", keys: keys, searchURL: tinyfishSearchEndpoint, fetchURL: tinyfishFetchEndpoint}
}

func (t *TinyFishSearchImpl) Name() string { return t.name }

func (t *TinyFishSearchImpl) Search(query string) (string, error) {
	results, err := t.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return t.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger，映射为 recency_minutes。
func (t *TinyFishSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	return t.search(context.Background(), query, lookbackDays)
}

// SearchRawContext 实现 core.ContextSearcher。
func (t *TinyFishSearchImpl) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	return t.search(ctx, query, 0)
}

// SearchRawWithTimeRangeContext 实现 core.ContextTimeRanger。
func (t *TinyFishSearchImpl) SearchRawWithTimeRangeContext(ctx context.Context, query string, lookbackDays int) ([]core.SearchResult, error) {
	return t.search(ctx, query, lookbackDays)
}

func (t *TinyFishSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	return t.search(context.Background(), query, 0)
}

func (t *TinyFishSearchImpl) search(ctx context.Context, query string, lookbackDays int) ([]core.SearchResult, error) {
	key := t.keys.Next()
	req := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("X-API-Key", key).
		SetQueryParam("query", query)
	if lookbackDays > 0 {
		req.SetQueryParam("recency_minutes", fmt.Sprintf("%d", lookbackDays*24*60))
	}
	var resp tinyfishSearchResp
	res, err := req.SetResult(&resp).Get(t.searchURL)
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("%s: %w", i18n.T("provider.tinyfish.call_failed"), err)}
	}
	if res.StatusCode() != http.StatusOK {
		err := errors.New(i18n.T("provider.tinyfish.bad_status", "Status", res.StatusCode()))
		if res.StatusCode() == http.StatusUnauthorized {
			return nil, &KeyError{Key: key, Err: err}
		}
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, errors.New(i18n.T("provider.tinyfish.empty_content"))
	}
	ret := make([]core.SearchResult, 0, len(resp.Results))
	for _, val := range resp.Results {
		item := core.SearchResult{
			Title:   strings.TrimSpace(val.Title),
			Url:     strings.TrimSpace(val.URL),
			Content: strings.TrimSpace(val.Snippet),
			Engine:  t.name,
		}
		if d := strings.TrimSpace(val.Date); d != "" {
			item.PublishDate = d
			item.DateSource = core.DateSourceStructured
		}
		ret = append(ret, item)
	}
	return ret, nil
}

// Fetch 通过 TinyFish Fetch API 抓取单页正文（markdown）。
// 免费额度按成功抓取的 URL 计数；单 URL 失败信息在 errors[] 里随 200 返回。
func (t *TinyFishSearchImpl) Fetch(ctx context.Context, rawURL string, maxChars int) (string, error) {
	key := t.keys.Next()
	var resp tinyfishFetchResp
	res, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("X-API-Key", key).
		SetHeader("Content-Type", "application/json").
		SetBody(tinyfishFetchReq{URLs: []string{rawURL}, Format: "markdown"}).
		SetResult(&resp).
		Post(t.fetchURL)
	if err != nil {
		return "", &KeyError{Key: key, Err: fmt.Errorf("%s: %w", i18n.T("provider.tinyfish.fetch_call_failed"), err)}
	}
	if res.StatusCode() != http.StatusOK {
		err := errors.New(i18n.T("provider.tinyfish.fetch_bad_status", "Status", res.StatusCode()))
		if res.StatusCode() == http.StatusUnauthorized {
			return "", &KeyError{Key: key, Err: err}
		}
		return "", err
	}
	if len(resp.Results) == 0 {
		if len(resp.Errors) > 0 {
			return "", errors.New(i18n.T("provider.tinyfish.fetch_failed", "Err", resp.Errors[0].Error))
		}
		return "", errors.New(i18n.T("provider.tinyfish.fetch_empty_results"))
	}
	text := strings.TrimSpace(resp.Results[0].Text)
	if text == "" {
		return "", errors.New(i18n.T("provider.tinyfish.fetch_empty_body"))
	}
	if maxChars > 0 {
		if runes := []rune(text); len(runes) > maxChars {
			text = string(runes[:maxChars])
		}
	}
	if title := strings.TrimSpace(resp.Results[0].Title); title != "" && !strings.HasPrefix(text, "# ") {
		text = "# " + title + "\n\n" + text
	}
	return text, nil
}

func (t *TinyFishSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
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

var (
	_ core.SearchInf         = (*TinyFishSearchImpl)(nil)
	_ core.ContextSearcher   = (*TinyFishSearchImpl)(nil)
	_ core.ContextTimeRanger = (*TinyFishSearchImpl)(nil)
	_ interface {
		Fetch(ctx context.Context, rawURL string, maxChars int) (string, error)
	} = (*TinyFishSearchImpl)(nil)
)
