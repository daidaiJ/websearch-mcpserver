package adapter

import (
	"websearch/pkg/search/core"

	"context"
	"fmt"
	"strings"
	"time"

	"websearch/pkg/antirobot"
)

// ──────────────────────────────────────────────────────────────────────────────
// EngineSearchAdapter 通用引擎适配器：antirobot.Engine → core.SearchInf
// ──────────────────────────────────────────────────────────────────────────────

// EngineSearchAdapter 将 antirobot.Engine 适配为 core.SearchInf 接口。
type EngineSearchAdapter struct {
	name     string
	searcher *antirobot.Searcher
}

// NewEngineSearchAdapter 创建通用引擎适配器。
func NewEngineSearchAdapter(name string, engines ...antirobot.Engine) *EngineSearchAdapter {
	searcher := antirobot.NewSearcher(antirobot.StrategySerial, engines)
	return &EngineSearchAdapter{name: name, searcher: searcher}
}

func (a *EngineSearchAdapter) Search(query string) (string, error) {
	results, err := a.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return a.MergeContent(query, results)
}

func (a *EngineSearchAdapter) SearchRaw(query string) ([]core.SearchResult, error) {
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
		return nil, fmt.Errorf("%s 搜索无结果", a.name)
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
			Engine:      a.name,
		})
	}
	return results, nil
}

func (a *EngineSearchAdapter) MergeContent(query string, results []core.SearchResult) (string, error) {
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
func (a *EngineSearchAdapter) Engines() []string {
	return a.searcher.Engines()
}

// Name 返回引擎适配器名称。
func (a *EngineSearchAdapter) Name() string {
	return a.name
}
