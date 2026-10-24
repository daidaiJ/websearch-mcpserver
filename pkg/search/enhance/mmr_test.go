package enhance

import (
	"websearch/pkg/search/core"
)

import "testing"

// ── Jaccard 相似度 ───────────────────────────────────────────────────────────

func TestJaccardSimilarity(t *testing.T) {
	// 相同文本 → 1.0
	if s := JaccardSimilarity("go generics performance", "go generics performance"); s < 0.99 {
		t.Errorf("相同文本相似度应接近 1.0, got %v", s)
	}
	// 完全不同 → 0.0
	if s := JaccardSimilarity("go generics", "baking bread recipes"); s != 0 {
		t.Errorf("无交集应返回 0, got %v", s)
	}
	// 部分重叠 → 0 < s < 1
	s := JaccardSimilarity("go generics performance guide", "go generics benchmark results")
	if s <= 0 || s >= 1 {
		t.Errorf("部分重叠应落在 (0,1), got %v", s)
	}
	// 空输入 → 0
	if s := JaccardSimilarity("", "anything"); s != 0 {
		t.Errorf("空输入应返回 0, got %v", s)
	}
	// 对称性
	a := JaccardSimilarity("a b c d", "b c d e")
	b := JaccardSimilarity("b c d e", "a b c d")
	if a != b {
		t.Errorf("Jaccard 应具有对称性: %v vs %v", a, b)
	}
}

// ── MMR 重排 ─────────────────────────────────────────────────────────────────

// TestApplyMMRTop1Protected Top-1 始终保留在首位。
func TestApplyMMRTop1Protected(t *testing.T) {
	results := []core.SearchResult{
		{Title: "A", Url: "https://a.com", Content: "go generics performance", Score: 0.9},
		{Title: "B", Url: "https://b.com", Content: "go generics performance", Score: 0.8},
		{Title: "C", Url: "https://c.com", Content: "unrelated topic content", Score: 0.7},
	}
	got := ApplyMMR(results, 0.7, 0)
	if len(got) == 0 || got[0].Url != "https://a.com" {
		t.Fatalf("Top-1 应保留在首位, got %+v", got)
	}
}

// TestApplyMMRDiversify MMR 应将高相似度结果后移，让不同主题提前。
func TestApplyMMRDiversify(t *testing.T) {
	// 3 条同主题高相似 + 1 条不同主题（score 略低）
	results := []core.SearchResult{
		{Title: "Stack Overflow Go Generics", Url: "https://stackoverflow.com/q/1", Content: "go generics performance benchmark question answer", Score: 0.9},
		{Title: "CSDN 转载 Go 泛型", Url: "https://csdn.example.com/go-generics", Content: "go generics performance benchmark 转载", Score: 0.85},
		{Title: "掘金镜像 Go 泛型", Url: "https://juejin.example.com/go-generics", Content: "go generics performance benchmark 镜像", Score: 0.80},
		{Title: "Go 官方泛型提案", Url: "https://go.dev/blog/generics", Content: "official go generics design proposal", Score: 0.78},
	}
	got := ApplyMMR(results, 0.7, 0)
	t.Logf("MMR 重排后顺序: %v", urlsOf(got))

	// 不同主题的 go.dev 应排在 3 条同主题内容之前（至少第 2 位）
	if got[1].Url != "https://go.dev/blog/generics" {
		t.Errorf("MMR 应将不同主题结果提前到第 2 位, got %q", got[1].Url)
	}
	// 三条同主题结果仍在列表中（不丢失结果）
	if len(got) != len(results) {
		t.Errorf("不截断时应保留全部结果, got %d", len(got))
	}
}

// TestApplyMMRLambdaOne lambda=1.0 时退化为纯相关性排序（原顺序）。
func TestApplyMMRLambdaOne(t *testing.T) {
	results := []core.SearchResult{
		{Title: "A", Url: "https://a.com", Content: "same topic content xyz", Score: 0.9},
		{Title: "B", Url: "https://b.com", Content: "same topic content xyz", Score: 0.8},
		{Title: "C", Url: "https://c.com", Content: "different topic", Score: 0.7},
	}
	got := ApplyMMR(results, 1.0, 0)
	for i := range results {
		if got[i].Url != results[i].Url {
			t.Errorf("lambda=1.0 应保持原顺序, got %v", urlsOf(got))
			break
		}
	}
}

// TestApplyMMRTargetCount targetN 截断。
func TestApplyMMRTargetCount(t *testing.T) {
	results := []core.SearchResult{
		{Title: "A", Url: "https://a.com", Content: "alpha beta gamma", Score: 0.9},
		{Title: "B", Url: "https://b.com", Content: "alpha beta gamma", Score: 0.8},
		{Title: "C", Url: "https://c.com", Content: "alpha beta gamma", Score: 0.7},
		{Title: "D", Url: "https://d.com", Content: "delta epsilon zeta", Score: 0.6},
	}
	got := ApplyMMR(results, 0.7, 3)
	if len(got) != 3 {
		t.Errorf("targetN=3 应返回 3 条, got %d", len(got))
	}
}

// urlsOf 提取结果 URL 列表（测试辅助）。
func urlsOf(results []core.SearchResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Url)
	}
	return out
}
