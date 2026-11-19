package ddg

import (
	"os"
	"strings"
	"testing"
)

// TestParseResultsRealSnapshot 用 2026-11-19 经代理抓取的真实 DDG 结果页
// 验证选择器、uddg 重定向解码与摘要提取。合成 fixture 仍覆盖广告过滤等
// 难以稳定抓到的分支。
func TestParseResultsRealSnapshot(t *testing.T) {
	body, err := os.ReadFile("testdata/ddg-serp-2026-11-19.html")
	if err != nil {
		t.Fatal(err)
	}
	e := &ddgEngine{}
	results := e.parseResults(string(body))
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Title != "The Go Programming Language" {
		t.Fatalf("first title = %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev/" {
		t.Fatalf("first url = %q", results[0].URL)
	}
	if !strings.Contains(results[0].Content, "open source programming language") {
		t.Fatalf("first snippet = %q", results[0].Content)
	}
	if results[1].URL != "https://go.dev/doc/install" {
		t.Fatalf("second url = %q", results[1].URL)
	}
	if results[2].URL != "https://www.runoob.com/go/go-tutorial.html" {
		t.Fatalf("third url = %q", results[2].URL)
	}
}
