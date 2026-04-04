package search

import (
	"fmt"
	"strings"
	"websearch/pkg/client"
	md "websearch/pkg/xml"
)

type TavilySearchImpl struct {
	apiKey    string
	blacklist []string
}

type tavilySearchReq struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth"`
	ExcludeDomains []string `json:"exclude_domains"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type tavilySearchResp struct {
	Results []tavilyResult `json:"results"`
}

func NewTavilySearch(apiKey string) *TavilySearchImpl {
	return &TavilySearchImpl{
		apiKey: apiKey,
	}
}

func (t *TavilySearchImpl) Search(query string) (string, error) {
	results, err := t.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return t.MergeContent(query, results)
}

// curl -X POST https://api.tavily.com/search \
// -H 'Content-Type: application/json' \
// -H 'Authorization: Bearer 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' \
// -d '{
//     "query": "",
//     "search_depth": "advanced"
// }'

func (t *TavilySearchImpl) SearchRaw(query string) ([]SearchResult, error) {
	req := tavilySearchReq{
		Query:          query,
		SearchDepth:    "basic",
		ExcludeDomains: t.blacklist,
	}
	var resp tavilySearchResp
	res, err := client.DefaultClient.R().
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", t.apiKey)).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post("https://api.tavily.com/search")
	if err != nil {
		return nil, fmt.Errorf("tavily 搜索api 调用失败，%w", err)
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("tavily 搜索api 返回错误状态码: %d", res.StatusCode())
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("tavily 搜索api 内容为空")
	}
	ret := make([]SearchResult, 0, len(resp.Results))
	for _, val := range resp.Results {
		ret = append(ret, SearchResult{
			Title:   val.Title,
			Url:     strings.TrimSpace(val.URL),
			Content: val.Content,
		})
	}
	return ret, nil
}

func (t *TavilySearchImpl) MergeContent(query string, results []SearchResult) (string, error) {

	if len(results) == 0 {

		return "", fmt.Errorf("没有搜索结果可以合并")

	}
	ret := md.MDSearchHeader(query, len(results))
	for i, val := range results {
		ret = fmt.Sprintf("%s%s", ret, md.FormatMD(i+1, val.Title, val.Url, val.Content))
	}
	return ret, nil

}
