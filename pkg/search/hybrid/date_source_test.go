package hybrid

import (
	"testing"

	"websearch/pkg/search/core"
)

// TestSearchRaw_DedupPrefersStructuredDate 跨引擎同 URL 去重时，
// 弱日期（snippet）应被强日期（structured）覆盖，来源引擎保持先到者。
func TestSearchRaw_DedupPrefersStructuredDate(t *testing.T) {
	first := &mockEngine{name: "baidu", results: []core.SearchResult{{
		Title: "同一页", Url: "https://example.com/a", Engine: "baidu",
		PublishDate: "2024-01-01", DateSource: core.DateSourceSnippet,
	}}}
	second := &mockEngine{name: "exa", results: []core.SearchResult{{
		Title: "同一页", Url: "https://example.com/a", Engine: "exa",
		PublishDate: "2024-02-02", DateSource: core.DateSourceStructured,
	}}}

	h := NewHybridSearch(first, second)
	got, err := h.SearchRaw("golang test")
	if err != nil {
		t.Fatalf("SearchRaw: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("同 URL 应去重为 1 条, got %d", len(got))
	}
	if got[0].PublishDate != "2024-02-02" || got[0].DateSource != core.DateSourceStructured {
		t.Errorf("弱日期应被结构化日期覆盖: %+v", got[0])
	}
	if got[0].Engine != "baidu" {
		t.Errorf("结果来源应保持先到引擎, got %q", got[0].Engine)
	}
}

// TestSearchRaw_DedupKeepsStructuredDate 反向：先到为结构化日期时不应被摘要日期降级。
func TestSearchRaw_DedupKeepsStructuredDate(t *testing.T) {
	first := &mockEngine{name: "exa", results: []core.SearchResult{{
		Title: "同一页", Url: "https://example.com/a", Engine: "exa",
		PublishDate: "2024-02-02", DateSource: core.DateSourceStructured,
	}}}
	second := &mockEngine{name: "baidu", results: []core.SearchResult{{
		Title: "同一页", Url: "https://example.com/a", Engine: "baidu",
		PublishDate: "2024-01-01", DateSource: core.DateSourceSnippet,
	}}}

	h := NewHybridSearch(first, second)
	got, err := h.SearchRaw("golang test")
	if err != nil {
		t.Fatalf("SearchRaw: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("同 URL 应去重为 1 条, got %d", len(got))
	}
	if got[0].PublishDate != "2024-02-02" || got[0].DateSource != core.DateSourceStructured {
		t.Errorf("结构化日期不应被弱日期覆盖: %+v", got[0])
	}
}
