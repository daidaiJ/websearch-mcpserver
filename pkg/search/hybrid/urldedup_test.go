package hybrid

import (
	"testing"
	"websearch/pkg/search/core"
)

// TestHybridSearch_CrossEngineURLVariantsMerge 同一页面的 scheme/尾斜杠/追踪参数
// 变体跨引擎出现时收敛为一条，只占一个结果名额。
func TestHybridSearch_CrossEngineURLVariantsMerge(t *testing.T) {
	e1 := &mockEngine{name: "baidu", results: []core.SearchResult{
		{Title: "canonical", Url: "https://example.com/page", Engine: "baidu"},
	}}
	e2 := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "tracking", Url: "https://example.com/page/?utm_source=bing&utm_campaign=x", Engine: "bing"},
	}}
	e3 := &mockEngine{name: "ddg", results: []core.SearchResult{
		{Title: "fragment", Url: "https://EXAMPLE.com/page#comments", Engine: "ddg"},
		{Title: "other", Url: "https://example.com/other", Engine: "ddg"},
	}}
	hs := NewHybridSearch(e1, e2, e3)

	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("同页三变体应合并为一条，共 2 条结果，实际 %d: %+v", len(results), results)
	}
	if results[0].Title != "canonical" {
		t.Errorf("应保留首条出现的结果，实际 %q", results[0].Title)
	}
}

// TestHybridSearch_SingleEngineURLVariantsMerge 单引擎内部同页变体同样收敛，
// 且计入 dedup 过滤统计。
func TestHybridSearch_SingleEngineURLVariantsMerge(t *testing.T) {
	e := &mockEngine{name: "baidu", results: []core.SearchResult{
		{Title: "first", Url: "https://example.com/page", Engine: "baidu"},
		{Title: "dup", Url: "https://example.com/page/?utm_medium=nav", Engine: "baidu"},
		{Title: "dup2", Url: "https://example.com/page#top", Engine: "baidu"},
	}}
	hs := NewHybridSearch(e)

	results, diag, err := hs.SearchRawDiagnostics("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Title != "first" {
		t.Fatalf("单引擎变体应只保留首条，实际 %+v", results)
	}
	if diag.FilterDrops[core.FilterDropDedup] != 2 {
		t.Errorf("dedup 丢弃数应为 2，实际 %d", diag.FilterDrops[core.FilterDropDedup])
	}
}
