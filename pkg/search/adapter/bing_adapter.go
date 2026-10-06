package adapter

import (
	"websearch/pkg/search/core"

	"context"
	"fmt"
	"strings"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/search/engine/bing"
)

// ──────────────────────────────────────────────────────────────────────────────
// BingSearchAdapter 适配 Bing 引擎到 core.SearchInf
// ──────────────────────────────────────────────────────────────────────────────

// BingSearchAdapter 将 Bing 引擎适配为 core.SearchInf 接口。
// 仅负责通用网页搜索，不包含学术搜索。
type BingSearchAdapter struct {
	searcher *antirobot.Searcher
}

// NewBingSearchAdapter 创建 Bing 搜索适配器。
func NewBingSearchAdapter(opts bing.BingOpts) *BingSearchAdapter {
	engines := []antirobot.Engine{bing.NewBing(opts)}
	searcher := antirobot.NewSearcher(antirobot.StrategySerial, engines)
	return &BingSearchAdapter{searcher: searcher}
}

func (a *BingSearchAdapter) Search(query string) (string, error) {
	results, err := a.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return a.MergeContent(query, results)
}

func (a *BingSearchAdapter) SearchRaw(query string) ([]core.SearchResult, error) {
	return a.doSearch(query)
}

func (a *BingSearchAdapter) Name() string { return "bing" }

func (a *BingSearchAdapter) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果")
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

// Engines 返回已注册的引擎名称列表。
func (a *BingSearchAdapter) Engines() []string {
	return a.searcher.Engines()
}

func (a *BingSearchAdapter) doSearch(query string) ([]core.SearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	responses := a.searcher.Search(ctx, query, 1)

	var all []antirobot.Result
	for _, resp := range responses {
		if resp.Error != "" {
			continue
		}
		all = append(all, resp.Results...)
	}
	all = antirobot.DeduplicateResults(all)

	if len(all) == 0 {
		return nil, fmt.Errorf("引擎搜索无结果")
	}

	results := make([]core.SearchResult, 0, len(all))
	for _, r := range all {
		results = append(results, core.SearchResult{
			Title:       r.Title,
			Url:         strings.TrimSpace(r.URL),
			Content:     r.Content,
			PublishDate: r.PublishedAt,
			DateSource:  r.DateSource,
			Score:       r.Score,
			Engine:      "bing",
		})
	}
	return results, nil
}
