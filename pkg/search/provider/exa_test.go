package provider

import (
	"strings"
	"testing"
	"websearch/pkg/search/core"
)

// ── 单元测试（不需要网络和 API Key） ──

func TestNewExaSearch(t *testing.T) {
	pool := mustNewKeyPool("test-key")
	exa := NewExaSearch(pool, []string{"spam.com"})
	if exa.Name() != "exa" {
		t.Errorf("expected name 'exa', got %s", exa.Name())
	}
	if exa.keys != pool {
		t.Error("expected keys to match pool")
	}
	if exa.numResults != 5 {
		t.Errorf("expected numResults 5, got %d", exa.numResults)
	}
	if exa.lookbackDays != 90 {
		t.Errorf("expected lookbackDays 90, got %d", exa.lookbackDays)
	}
	if len(exa.excludeDomains) != 1 || exa.excludeDomains[0] != "spam.com" {
		t.Errorf("unexpected excludeDomains: %v", exa.excludeDomains)
	}
}

func TestNewExaSearchWithResults(t *testing.T) {
	pool := mustNewKeyPool("key")
	tests := []struct {
		name         string
		numResults   int
		lookbackDays int
		wantNum      int
		wantDays     int
	}{
		{"defaults", 0, 0, 5, 90},
		{"custom", 10, 30, 10, 30},
		{"negative", -1, -5, 5, 90},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exa := NewExaSearchWithResults(pool, tt.numResults, tt.lookbackDays, nil)
			if exa.numResults != tt.wantNum {
				t.Errorf("numResults: got %d, want %d", exa.numResults, tt.wantNum)
			}
			if exa.lookbackDays != tt.wantDays {
				t.Errorf("lookbackDays: got %d, want %d", exa.lookbackDays, tt.wantDays)
			}
		})
	}
}

func TestExaSearchImpl_MergeContent(t *testing.T) {
	pool := mustNewKeyPool("key")
	exa := NewExaSearch(pool, nil)
	results := []core.SearchResult{
		{Title: "Result 1", Url: "https://example.com/1", Content: "Content 1", Engine: "exa"},
		{Title: "Result 2", Url: "https://example.com/2", Content: "Content 2", Engine: "exa"},
	}

	// 测试 core.ShowMeta = true
	core.ShowMeta = true
	output, err := exa.MergeContent("test query", results)
	if err != nil {
		t.Fatalf("MergeContent failed: %v", err)
	}
	if !strings.Contains(output, "test query") {
		t.Error("output should contain query")
	}
	if !strings.Contains(output, "Result 1") {
		t.Error("output should contain first result title")
	}
	if !strings.Contains(output, "**来源**: exa") {
		t.Error("output should contain engine source when core.ShowMeta=true")
	}

	// 测试 core.ShowMeta = false
	core.ShowMeta = false
	output, err = exa.MergeContent("test query", results)
	if err != nil {
		t.Fatalf("MergeContent failed: %v", err)
	}
	if strings.Contains(output, "**来源**") {
		t.Error("output should not contain source field when core.ShowMeta=false")
	}

	// 恢复默认值
	core.ShowMeta = true
}

func TestExaSearchImpl_MergeContent_Empty(t *testing.T) {
	pool := mustNewKeyPool("key")
	exa := NewExaSearch(pool, nil)
	_, err := exa.MergeContent("test", nil)
	if err == nil {
		t.Error("expected error for empty results")
	}
}

// mustNewKeyPool 测试辅助：创建单 key KeyPool，失败时 panic。
func mustNewKeyPool(key string) *KeyPool {
	pool, err := NewKeyPool([]string{key})
	if err != nil {
		panic(err)
	}
	return pool
}

// ── 集成测试（从 config.test.yaml 加载 API Key） ──

func TestExaSearchImpl_SearchRaw_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadExaAPIKey(t)

	exa := NewExaSearchWithResults(newTestKeyPool(t, apiKey), 3, 30, []string{"csdn.net"})
	results, err := exa.SearchRaw("Go programming language")
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	for i, r := range results {
		t.Logf("[%d] %s - %s", i+1, r.Title, r.Url)
		if r.Title == "" {
			t.Error("result title should not be empty")
		}
		if r.Url == "" {
			t.Error("result url should not be empty")
		}
		if r.Engine != "exa" {
			t.Errorf("expected engine 'exa', got %s", r.Engine)
		}
	}
}

func TestExaSearchImpl_Search_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadExaAPIKey(t)

	exa := NewExaSearch(newTestKeyPool(t, apiKey), nil)
	output, err := exa.Search("latest AI news")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if output == "" {
		t.Fatal("expected non-empty output")
	}
	t.Logf("Search output:\n%s", output)
}

func TestExaSearchImpl_SearchRawWithTimeRange_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadExaAPIKey(t)

	exa := NewExaSearchWithResults(newTestKeyPool(t, apiKey), 3, 90, nil)

	// 测试动态时间范围（7天）
	results, err := exa.SearchRawWithTimeRange("AI news", 7)
	if err != nil {
		t.Fatalf("SearchRawWithTimeRange failed: %v", err)
	}
	t.Logf("7天范围结果数: %d", len(results))

	// 测试不限时间范围
	results, err = exa.SearchRawWithTimeRange("AI news", 0)
	if err != nil {
		t.Fatalf("SearchRawWithTimeRange with no filter failed: %v", err)
	}
	t.Logf("不限时间范围结果数: %d", len(results))
}
