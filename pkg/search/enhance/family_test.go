package enhance

import (
	"testing"
	"websearch/pkg/search/core"
)

// TestEngineFamily 引擎家族映射：同上游引擎归并到 baidu，其余引擎名即自身 family。
func TestEngineFamily(t *testing.T) {
	cases := map[string]string{
		"baidu_api": "baidu",
		"baidu_ai":  "baidu",
		"baidu_web": "baidu",
		"baidu":     "baidu",
		"bing":      "bing",
		"ddg":       "ddg",
	}
	for name, want := range cases {
		if got := EngineFamily(name); got != want {
			t.Fatalf("EngineFamily(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestConsensusFamilyDedup 百度网页引擎与百度千帆 API 是同一上游：同一 URL 两边
// 命中时按 family 计票不得双计共识——总分应低于"baidu + bing"（不同上游）的组合。
func TestConsensusFamilyDedup(t *testing.T) {
	mk := func(name, url string) core.ScoreBucket {
		return core.ScoreBucket{Name: name, Results: []core.SearchResult{
			{Title: "golang release", Url: url, Content: "release notes", Engine: name},
		}}
	}
	sameUpstream := EnhanceResults("golang release", []core.ScoreBucket{
		mk("baidu", "http://example.com/go"),
		mk("baidu_api", "http://example.com/go"),
	}, 0, 10)
	diffUpstream := EnhanceResults("golang release", []core.ScoreBucket{
		mk("baidu", "http://example.com/go"),
		mk("bing", "http://example.com/go"),
	}, 0, 10)

	if len(sameUpstream) != 1 || len(diffUpstream) != 1 {
		t.Fatalf("两组都应各产出 1 条，实际 %d / %d", len(sameUpstream), len(diffUpstream))
	}
	// RRF 分量相同（同排名同权重），差异只来自共识 Boost
	if sameUpstream[0].Score >= diffUpstream[0].Score {
		t.Fatalf("同上游双引擎不应获得共识加分: same=%v diff=%v", sameUpstream[0].Score, diffUpstream[0].Score)
	}
}
