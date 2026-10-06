package hybrid

import (
	"websearch/pkg/search/core"

	"bytes"
	"fmt"
	"strings"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/log"
)

// ── mock core.SearchInf ───────────────────────────────────────────────────────────

type mockEngine struct {
	name    string
	results []core.SearchResult
	err     error
}

func (m *mockEngine) Name() string { return m.name }
func (m *mockEngine) Search(query string) (string, error) {
	return "", nil
}
func (m *mockEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}
func (m *mockEngine) MergeContent(query string, results []core.SearchResult) (string, error) {
	return "", nil
}

// ── SortByScore ─────────────────────────────────────────────────────────────

func TestSortByScore(t *testing.T) {
	results := []core.SearchResult{
		{Title: "low", Score: 0.1, Engine: "a"},
		{Title: "high", Score: 0.9, Engine: "a"},
		{Title: "mid", Score: 0.5, Engine: "a"},
		{Title: "zero", Score: 0, Engine: "a"},
	}
	core.SortByScore(results)
	if results[0].Title != "high" {
		t.Errorf("expected high first, got %s", results[0].Title)
	}
	if results[1].Title != "mid" {
		t.Errorf("expected mid second, got %s", results[1].Title)
	}
	if results[2].Title != "low" {
		t.Errorf("expected low third, got %s", results[2].Title)
	}
}

// ── FilterByScore ───────────────────────────────────────────────────────────

func TestFilterByScore_WithScores(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a", Score: 0.8, Engine: "tavily_api"},
		{Title: "b", Score: 0.3, Engine: "tavily_api"},
		{Title: "c", Score: 0.6, Engine: "tavily_api"},
	}
	filtered := core.FilterByScore(results, 0.5)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 results, got %d", len(filtered))
	}
	if filtered[0].Title != "a" || filtered[1].Title != "c" {
		t.Errorf("unexpected results: %v", filtered)
	}
}

func TestFilterByScore_NoScores_Ignored(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a", Score: 0, Engine: "bing"},
		{Title: "b", Score: 0, Engine: "bing"},
		{Title: "c", Score: 0, Engine: "bing"},
	}
	filtered := core.FilterByScore(results, 0.5)
	if len(filtered) != 3 {
		t.Fatalf("no-score engine should not be filtered, got %d", len(filtered))
	}
}

func TestFilterByScore_ZeroThreshold(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a", Score: 0.8},
		{Title: "b", Score: 0.1},
	}
	filtered := core.FilterByScore(results, 0)
	if len(filtered) != 2 {
		t.Fatalf("zero threshold should not filter, got %d", len(filtered))
	}
}

// ── DistributeResults ───────────────────────────────────────────────────────

func TestDistributeResults_EvenDistribution(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a1", Engine: "a"}, {Title: "a2", Engine: "a"}, {Title: "a3", Engine: "a"},
		{Title: "b1", Engine: "b"}, {Title: "b2", Engine: "b"}, {Title: "b3", Engine: "b"},
	}
	out := DistributeResults(results, 4)
	if len(out) != 4 {
		t.Fatalf("expected 4, got %d", len(out))
	}
	// 轮询: a1, b1, a2, b2
	if out[0].Title != "a1" || out[1].Title != "b1" || out[2].Title != "a2" || out[3].Title != "b2" {
		t.Errorf("unexpected round-robin order: %v", []string{out[0].Title, out[1].Title, out[2].Title, out[3].Title})
	}
}

func TestDistributeResults_LimitExceedsTotal(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a1", Engine: "a"},
		{Title: "b1", Engine: "b"},
	}
	out := DistributeResults(results, 10)
	if len(out) != 2 {
		t.Fatalf("should return all when limit > len, got %d", len(out))
	}
}

func TestDistributeResults_SingleEngine(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a1", Engine: "a"}, {Title: "a2", Engine: "a"}, {Title: "a3", Engine: "a"},
	}
	out := DistributeResults(results, 2)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if out[0].Title != "a1" || out[1].Title != "a2" {
		t.Errorf("unexpected: %v", out)
	}
}

func TestDistributeResults_EmptyEngine(t *testing.T) {
	results := []core.SearchResult{
		{Title: "a1", Engine: ""}, {Title: "b1", Engine: "b"},
	}
	out := DistributeResults(results, 2)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
}

// ── core.FormatScore ─────────────────────────────────────────────────────────────

func TestFormatScore(t *testing.T) {
	if s := core.FormatScore(0.8765); s != "0.8765" {
		t.Errorf("expected 0.8765, got %s", s)
	}
	if s := core.FormatScore(0); s != "" {
		t.Errorf("expected empty for 0, got %s", s)
	}
	if s := core.FormatScore(-1); s != "" {
		t.Errorf("expected empty for negative, got %s", s)
	}
}

// ── HybridSearchImpl.SearchRaw ──────────────────────────────────────────────

func TestHybridSearch_BasicMerge(t *testing.T) {
	e1 := &mockEngine{name: "a", results: []core.SearchResult{
		{Title: "a1", Url: "http://a1.com", Score: 0.9, Engine: "a"},
		{Title: "a2", Url: "http://a2.com", Score: 0.7, Engine: "a"},
	}}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0.8, Engine: "b"},
		{Title: "b2", Url: "http://b2.com", Score: 0.6, Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4, got %d", len(results))
	}
}

func TestHybridSearch_DedupAcrossEngines(t *testing.T) {
	e1 := &mockEngine{name: "a", results: []core.SearchResult{
		{Title: "a1", Url: "http://shared.com", Score: 0.9, Engine: "a"},
	}}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://shared.com", Score: 0.8, Engine: "b"},
		{Title: "b2", Url: "http://b2.com", Score: 0.6, Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 after dedup, got %d", len(results))
	}
	// 保留先出现的 a 的结果
	if results[0].Engine != "a" {
		t.Errorf("expected engine a first, got %s", results[0].Engine)
	}
}

func TestHybridSearch_ScoreFilter(t *testing.T) {
	e1 := &mockEngine{name: "tavily_api", results: []core.SearchResult{
		{Title: "high", Url: "http://high.com", Score: 0.9, Engine: "tavily_api"},
		{Title: "low", Url: "http://low.com", Score: 0.2, Engine: "tavily_api"},
		{Title: "mid", Url: "http://mid.com", Score: 0.6, Engine: "tavily_api"},
	}}
	e2 := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0, Engine: "bing"},
	}}
	hs := NewHybridSearch(e1, e2)
	hs.SetFilters(map[string]EngineFilter{
		"tavily_api": {MinScore: 0.5},
	})
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tavily_api: high(0.9) + mid(0.6) pass, low(0.2) filtered; bing: no score, no filter
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	for _, r := range results {
		if r.Engine == "tavily_api" && r.Score < 0.5 {
			t.Errorf("tavily result with score %f should have been filtered", r.Score)
		}
	}
}

func TestHybridSearch_NoScoreEngine_IgnoresMinScore(t *testing.T) {
	e1 := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0, Engine: "bing"},
		{Title: "b2", Url: "http://b2.com", Score: 0, Engine: "bing"},
	}}
	hs := NewHybridSearch(e1)
	hs.SetFilters(map[string]EngineFilter{
		"bing": {MinScore: 0.9}, // should be ignored since bing returns no scores
	})
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("no-score engine should ignore minScore, got %d", len(results))
	}
}

func TestHybridSearch_PerEngineMaxSize(t *testing.T) {
	e1 := &mockEngine{name: "a", results: []core.SearchResult{
		{Title: "a1", Url: "http://a1.com", Score: 0.9, Engine: "a"},
		{Title: "a2", Url: "http://a2.com", Score: 0.8, Engine: "a"},
		{Title: "a3", Url: "http://a3.com", Score: 0.7, Engine: "a"},
		{Title: "a4", Url: "http://a4.com", Score: 0.6, Engine: "a"},
		{Title: "a5", Url: "http://a5.com", Score: 0.5, Engine: "a"},
	}}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0.8, Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	hs.SetFilters(map[string]EngineFilter{
		"a": {MaxSize: 2},
	})
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// a truncated to 2, b has 1 → total 3
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	countA := 0
	for _, r := range results {
		if r.Engine == "a" {
			countA++
		}
	}
	if countA != 2 {
		t.Errorf("expected 2 from engine a, got %d", countA)
	}
}

func TestHybridSearch_GlobalMaxSize_WithScore(t *testing.T) {
	e1 := &mockEngine{name: "a", results: []core.SearchResult{
		{Title: "a1", Url: "http://a1.com", Score: 0.9, Engine: "a"},
		{Title: "a2", Url: "http://a2.com", Score: 0.3, Engine: "a"},
	}}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0.7, Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	hs.SetMaxSize(2)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	// 按 score 排序后截断: a1(0.9), b1(0.7)
	if results[0].Title != "a1" || results[1].Title != "b1" {
		t.Errorf("expected a1,b1 by score, got %s,%s", results[0].Title, results[1].Title)
	}
}

func TestHybridSearch_GlobalMaxSize_NoScore_Fallback(t *testing.T) {
	e1 := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0, Engine: "bing"},
		{Title: "b2", Url: "http://b2.com", Score: 0, Engine: "bing"},
		{Title: "b3", Url: "http://b3.com", Score: 0, Engine: "bing"},
		{Title: "b4", Url: "http://b4.com", Score: 0, Engine: "bing"},
	}}
	e2 := &mockEngine{name: "google", results: []core.SearchResult{
		{Title: "g1", Url: "http://g1.com", Score: 0, Engine: "google"},
		{Title: "g2", Url: "http://g2.com", Score: 0, Engine: "google"},
		{Title: "g3", Url: "http://g3.com", Score: 0, Engine: "google"},
		{Title: "g4", Url: "http://g4.com", Score: 0, Engine: "google"},
	}}
	hs := NewHybridSearch(e1, e2)
	hs.SetMaxSize(3) // ceil(3/2) = 2 per engine → 2+2 = 4, but global max = 3
	// per-engine: default 4, fallback cap = ceil(3/2) = 2, min(4,2) = 2
	// each engine contributes 2 → 4 total → global max 3 → round-robin to 3
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	// round-robin: b1, g1, b2
	if results[0].Title != "b1" || results[1].Title != "g1" || results[2].Title != "b2" {
		t.Errorf("unexpected round-robin: %s,%s,%s", results[0].Title, results[1].Title, results[2].Title)
	}
}

func TestHybridSearch_AllEnginesFail(t *testing.T) {
	e1 := &mockEngine{name: "a", err: fmt.Errorf("fail")}
	e2 := &mockEngine{name: "b", err: fmt.Errorf("fail")}
	hs := NewHybridSearch(e1, e2)
	_, err := hs.SearchRaw("test")
	if err == nil {
		t.Fatal("expected error when all engines fail")
	}
}

func TestHybridSearch_OneEngineFails_OtherSucceeds(t *testing.T) {
	e1 := &mockEngine{name: "a", err: fmt.Errorf("fail")}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0.8, Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Title != "b1" {
		t.Errorf("expected b1, got %s", results[0].Title)
	}
}

// TestHybridSearch_EngineFailureLogged 单引擎失败应打 Warn 日志（编排层，不泄漏 Key）。
func TestHybridSearch_EngineFailureLogged(t *testing.T) {
	var buf bytes.Buffer
	log.NewLoggerTo(&buf, "", config.LogConfig{})
	defer log.CloseFile()

	e1 := &mockEngine{name: "a", err: fmt.Errorf("boom")}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if !strings.Contains(buf.String(), "hybrid: engine a failed") {
		t.Errorf("expected warn log for engine a, got: %q", buf.String())
	}
}

// TestHybridSearch_AllFail_ErrorSummarizesEngines 全失败时 error 拼接各引擎错误摘要
// （统一契约格式：name(kind): 短原因）。
func TestHybridSearch_AllFail_ErrorSummarizesEngines(t *testing.T) {
	e1 := &mockEngine{name: "a", err: fmt.Errorf("fail1")}
	e2 := &mockEngine{name: "b", err: fmt.Errorf("fail2")}
	hs := NewHybridSearch(e1, e2)
	_, err := hs.SearchRaw("test")
	if err == nil {
		t.Fatal("expected error when all engines fail")
	}
	if !strings.Contains(err.Error(), "a(error): fail1") || !strings.Contains(err.Error(), "b(error): fail2") {
		t.Errorf("expected engine error summary, got: %v", err)
	}
}

// ── HybridSearchImpl.MergeContent ───────────────────────────────────────────

func TestMergeContent_ShowMeta(t *testing.T) {
	oldShowMeta := core.ShowMeta
	core.ShowMeta = true
	defer func() { core.ShowMeta = oldShowMeta }()

	hs := NewHybridSearch(&mockEngine{name: "a"})
	results := []core.SearchResult{
		{Title: "Test", Url: "http://test.com", Content: "body", Engine: "tavily_api", Score: 0.85},
	}
	out, err := hs.MergeContent("query", results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "tavily_api") {
		t.Error("expected engine name in output")
	}
	if !strings.Contains(out, "0.85") {
		t.Error("expected score in output")
	}
}

func TestMergeContent_HideMeta(t *testing.T) {
	oldShowMeta := core.ShowMeta
	core.ShowMeta = false
	defer func() { core.ShowMeta = oldShowMeta }()

	hs := NewHybridSearch(&mockEngine{name: "a"})
	results := []core.SearchResult{
		{Title: "Test", Url: "http://test.com", Content: "body", Engine: "tavily_api", Score: 0.85},
	}
	out, err := hs.MergeContent("query", results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "来源") {
		t.Error("show_meta=false should not contain engine info")
	}
	if strings.Contains(out, "相关性") {
		t.Error("show_meta=false should not contain score info")
	}
}

// ── HybridSearchImpl per-engine maxsize with no-score fallback cap ──────────

func TestHybridSearch_PerEngineMaxSize_NoScore_FallbackCap(t *testing.T) {
	// 3 engines, global maxSize=6, per-engine maxSize=10 (default 4)
	// no-score fallback: min(4, ceil(6/3)) = min(4, 2) = 2
	e1 := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0, Engine: "bing"},
		{Title: "b2", Url: "http://b2.com", Score: 0, Engine: "bing"},
		{Title: "b3", Url: "http://b3.com", Score: 0, Engine: "bing"},
	}}
	e2 := &mockEngine{name: "google", results: []core.SearchResult{
		{Title: "g1", Url: "http://g1.com", Score: 0, Engine: "google"},
		{Title: "g2", Url: "http://g2.com", Score: 0, Engine: "google"},
		{Title: "g3", Url: "http://g3.com", Score: 0, Engine: "google"},
	}}
	e3 := &mockEngine{name: "baidu_api", results: []core.SearchResult{
		{Title: "bd1", Url: "http://bd1.com", Score: 0, Engine: "baidu_api"},
		{Title: "bd2", Url: "http://bd2.com", Score: 0, Engine: "baidu_api"},
		{Title: "bd3", Url: "http://bd3.com", Score: 0, Engine: "baidu_api"},
	}}
	hs := NewHybridSearch(e1, e2, e3)
	hs.SetMaxSize(6)
	// default EngineFilter (no per-engine config) → maxSize=0 → use default 4
	// no-score fallback: min(4, ceil(6/3)) = min(4, 2) = 2
	// each engine contributes 2 → 6 total → exactly global max
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 6 {
		t.Fatalf("expected 6, got %d", len(results))
	}
	// count per engine
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Engine]++
	}
	for _, name := range []string{"bing", "google", "baidu_api"} {
		if counts[name] != 2 {
			t.Errorf("expected 2 from %s, got %d", name, counts[name])
		}
	}
}

// ── Name() implementations ──────────────────────────────────────────────────

func TestHybridSearch_Name(t *testing.T) {
	hs := NewHybridSearch(&mockEngine{name: "a"}, &mockEngine{name: "b"})
	if hs.Name() != "hybrid" {
		t.Errorf("expected hybrid, got %s", hs.Name())
	}
}

// ── Edge: empty results from one engine ─────────────────────────────────────

func TestHybridSearch_OneEngineEmpty(t *testing.T) {
	e1 := &mockEngine{name: "a", results: []core.SearchResult{}}
	e2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0.8, Engine: "b"},
	}}
	hs := NewHybridSearch(e1, e2)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

// ── Mixed score/no-score engines ────────────────────────────────────────────

func TestHybridSearch_MixedScoreNoScore(t *testing.T) {
	e1 := &mockEngine{name: "tavily_api", results: []core.SearchResult{
		{Title: "t1", Url: "http://t1.com", Score: 0.9, Engine: "tavily_api"},
		{Title: "t2", Url: "http://t2.com", Score: 0.7, Engine: "tavily_api"},
		{Title: "t3", Url: "http://t3.com", Score: 0.3, Engine: "tavily_api"},
	}}
	e2 := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "b1", Url: "http://b1.com", Score: 0, Engine: "bing"},
		{Title: "b2", Url: "http://b2.com", Score: 0, Engine: "bing"},
	}}
	hs := NewHybridSearch(e1, e2)
	hs.SetFilters(map[string]EngineFilter{
		"tavily_api": {MinScore: 0.5, MaxSize: 5},
		"bing":       {MaxSize: 3},
	})
	hs.SetMaxSize(4)
	results, err := hs.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// tavily_api: t1(0.9), t2(0.7) pass minScore; t3(0.3) filtered → 2 results
	// bing: no score, no minScore filter; fallback cap = min(3, ceil(4/2)) = min(3,2) = 2 → 2 results
	// merged: 4 total, ≤ global max 4 → no global truncation
	if len(results) != 4 {
		t.Fatalf("expected 4, got %d", len(results))
	}
	// verify tavily results all have score >= 0.5
	for _, r := range results {
		if r.Engine == "tavily_api" && r.Score < 0.5 {
			t.Errorf("tavily_api result with score %f should have been filtered", r.Score)
		}
	}
}
