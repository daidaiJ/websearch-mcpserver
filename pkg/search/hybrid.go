package search

import (
	"fmt"
	"strings"
	"sync"
	md "websearch/pkg/xml"
)

type HybridSearchImpl struct {
	engines []SearchInf
}

func NewHybridSearch(engines ...SearchInf) *HybridSearchImpl {
	return &HybridSearchImpl{engines: engines}
}

func (h *HybridSearchImpl) Search(query string) (string, error) {
	results, err := h.SearchRaw(query)
	if err != nil {
		return "", err
	}
	ret := md.MDSearchHeader(query, len(results))
	for i, val := range results {
		ret = fmt.Sprintf("%s%s", ret, md.FormatMD(i+1, val.Title, val.Url, val.Content))
	}
	return ret, nil
}

func (h *HybridSearchImpl) SearchRaw(query string) ([]SearchResult, error) {
	type indexedResult struct {
		index   int
		results []SearchResult
		err     error
	}

	var wg sync.WaitGroup
	ch := make(chan indexedResult, len(h.engines))

	for i, engine := range h.engines {
		wg.Add(1)
		go func(idx int, e SearchInf) {
			defer wg.Done()
			results, err := e.SearchRaw(query)
			ch <- indexedResult{index: idx, results: results, err: err}
		}(i, engine)
	}

	wg.Wait()
	close(ch)

	seen := make(map[string]struct{})
	var merged []SearchResult

	// 收集所有成功的结果，按引擎顺序合并
	var allResults []indexedResult
	for r := range ch {
		if r.err != nil {
			continue // 忽略单个引擎失败，只要有一个成功就行
		}
		allResults = append(allResults, r)
	}
	if len(allResults) == 0 {
		return nil, fmt.Errorf("所有搜索引擎均失败")
	}

	// 按 index 排序保证结果顺序稳定
	for i := 0; i < len(allResults); i++ {
		for j := i + 1; j < len(allResults); j++ {
			if allResults[i].index > allResults[j].index {
				allResults[i], allResults[j] = allResults[j], allResults[i]
			}
		}
	}
	// 合并结果，去重
	for _, ir := range allResults {
		for _, r := range ir.results {
			normalizedURL := strings.TrimSpace(r.Url)
			if _, exists := seen[normalizedURL]; exists {
				continue
			}
			seen[normalizedURL] = struct{}{}
			merged = append(merged, r)
		}
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("所有搜索引擎均未返回有效结果")
	}
	return merged, nil
}

func (h *HybridSearchImpl) MergeContent(query string, results []SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有结果可合并")
	}
	ret := md.MDSearchHeader(query, len(results))
	for i, val := range results {
		ret = fmt.Sprintf("%s%s", ret, md.FormatMD(i+1, val.Title, val.Url, val.Content))
	}
	return ret, nil
}
