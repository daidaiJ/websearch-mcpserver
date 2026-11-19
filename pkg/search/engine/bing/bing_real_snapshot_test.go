package bing

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseRealSERPSnapshot 用 2026-11-19 抓取并脱敏的 Bing 结果页快照回归。
// 与合成 fixture 相比，这份样本保留了 b_tpcn / b_attribution / b_algoReadMore
// 等真实包装，能在上游改版时更早暴露选择器漂移。
func TestParseRealSERPSnapshot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "bing-serp-2026-11-19.html"))
	if err != nil {
		t.Fatal(err)
	}
	e := &bingEngine{}
	results := e.parseResults(string(raw))
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3: %+v", len(results), results)
	}
	if results[0].Title != "Go 1.26 release notes" {
		t.Fatalf("first title = %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev/doc/go1.26" {
		t.Fatalf("first url = %q", results[0].URL)
	}
	if results[2].URL != "https://example.com/redirected" {
		t.Fatalf("redirect url = %q, want decoded target", results[2].URL)
	}
}
