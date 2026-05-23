package search

import (
	"context"
	"fmt"
	"strings"
	"time"
	"websearch/pkg/bing"
	md "websearch/pkg/xml"
)

// ──────────────────────────────────────────────────────────────────────────────
// BingSearchAdapter 适配 bing.Searcher 到 SearchInf + AcademicSearcher
// ──────────────────────────────────────────────────────────────────────────────

// BingSearchAdapter 将 bing 包的多引擎搜索器适配为标准 SearchInf 接口。
// 同时实现 AcademicSearcher 接口，支持学术论文检索。
type BingSearchAdapter struct {
	regular  *bing.Searcher // Bing 通用搜索（不含学术引擎）
	academic *bing.Searcher // 学术搜索（含 Bing + 学术引擎），可为 nil
}

// NewBingSearchAdapter 创建适配器。
// regularOpts 用于通用搜索，academicOpts 用于学术搜索（Academic=true）。
// 若 academicOpts 未启用学术引擎，academic 搜索器为 nil。
func NewBingSearchAdapter(regularOpts, academicOpts bing.Options) *BingSearchAdapter {
	a := &BingSearchAdapter{
		regular: bing.NewSearcher(regularOpts),
	}
	if academicOpts.Academic {
		a.academic = bing.NewSearcher(academicOpts)
	}
	return a
}

func (a *BingSearchAdapter) Search(query string) (string, error) {
	results, err := a.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return a.MergeContent(query, results)
}

func (a *BingSearchAdapter) SearchRaw(query string) ([]SearchResult, error) {
	return a.doSearch(a.regular, query)
}

func (a *BingSearchAdapter) MergeContent(query string, results []SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(md.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if val.Type == "paper" {
			buf.WriteString(md.FormatPaperMD(i+1, val.Title, val.Url, val.Authors, val.DOI, val.Content))
		} else {
			buf.WriteString(md.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}

// SearchAcademicRaw 实现 AcademicSearcher 接口，返回学术论文搜索结果。
func (a *BingSearchAdapter) SearchAcademicRaw(query string) ([]SearchResult, error) {
	if a.academic == nil {
		return nil, fmt.Errorf("学术搜索引擎未启用")
	}
	return a.doSearch(a.academic, query)
}

// doSearch 执行实际搜索并将 bing.Result 转换为 SearchResult。
func (a *BingSearchAdapter) doSearch(s *bing.Searcher, query string) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	responses := s.Search(ctx, query, 1)

	var all []bing.Result
	for _, resp := range responses {
		if resp.Error != "" {
			continue
		}
		all = append(all, resp.Results...)
	}
	all = bing.DeduplicateResults(all)
	all = bing.NormalizeAndSortResults(all)

	if len(all) == 0 {
		return nil, fmt.Errorf("引擎搜索无结果")
	}

	results := make([]SearchResult, 0, len(all))
	for _, r := range all {
		results = append(results, SearchResult{
			Title:       r.Title,
			Url:         strings.TrimSpace(r.URL),
			Content:     r.Content,
			PublishDate: r.PublishedAt,
			Type:        string(r.Type),
			Authors:     r.Authors,
			DOI:         r.DOI,
		})
	}
	return results, nil
}

// Engines 返回通用搜索已注册的引擎名称列表。
func (a *BingSearchAdapter) Engines() []string {
	return a.regular.Engines()
}

// AcademicEngines 返回学术搜索已注册的引擎名称列表。
func (a *BingSearchAdapter) AcademicEngines() []string {
	if a.academic == nil {
		return nil
	}
	return a.academic.Engines()
}
