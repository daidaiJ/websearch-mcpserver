package hybrid

import (
	"errors"
	"strings"
	"testing"
	"websearch/pkg/config"
	"websearch/pkg/search/core"
)

// ── 失败透出统一契约（core.DiagnosticsProvider） ──────────────────────────────

// TestHybridSearch_PartialFailureDiagnostics 部分引擎失败时，失败清单应带
// 分类（rate_limit 等）与短原因透出，成功引擎结果正常返回。
func TestHybridSearch_PartialFailureDiagnostics(t *testing.T) {
	e1 := &mockEngine{name: "baidu", results: []core.SearchResult{
		{Title: "a1", Url: "http://a1.com", Engine: "baidu"},
	}}
	e2 := &mockEngine{name: "ddg", err: errors.New("ddg: HTTP 429 rate-limited (cooling down 30s)")}
	hs := NewHybridSearch(e1, e2)
	results, err := hs.SearchRaw("golang release")
	if err != nil {
		t.Fatalf("部分引擎失败不应整体报错: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	diag := hs.LastDiagnostics()
	if !diag.HasFailures() {
		t.Fatal("失败清单不应为空")
	}
	f := diag.Failures[0]
	if f.Engine != "ddg" || f.Kind != core.FailureRateLimit {
		t.Fatalf("失败应归类为 ddg/rate_limit，实际 %+v", f)
	}
	if f.Reason == "" {
		t.Fatal("短原因不应为空")
	}
}

// TestHybridSearch_AllFailErrorClassified 全部失败时错误信息应带逐引擎分类摘要。
func TestHybridSearch_AllFailErrorClassified(t *testing.T) {
	e1 := &mockEngine{name: "a", err: errors.New("context deadline exceeded")}
	e2 := &mockEngine{name: "b", err: errors.New("被反爬拦截，返回验证码")}
	hs := NewHybridSearch(e1, e2)
	_, err := hs.SearchRaw("golang release")
	if err == nil {
		t.Fatal("全部失败应返回错误")
	}
	for _, want := range []string{"a(timeout)", "b(challenge)"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息应含 %s: %v", want, err)
		}
	}
}

// TestHybridSearch_FilterDropsTracked 过滤丢弃数应进诊断（min_score / engine_max_size）。
func TestHybridSearch_FilterDropsTracked(t *testing.T) {
	e1 := &mockEngine{name: "tavily_api", results: []core.SearchResult{
		{Title: "high", Url: "http://h.com", Score: 0.9, Engine: "tavily_api"},
		{Title: "low1", Url: "http://l1.com", Score: 0.2, Engine: "tavily_api"},
		{Title: "low2", Url: "http://l2.com", Score: 0.1, Engine: "tavily_api"},
	}}
	hs := NewHybridSearch(e1)
	hs.SetFilters(map[string]EngineFilter{"tavily_api": {MinScore: 0.5}})
	if _, err := hs.SearchRaw("golang release"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	diag := hs.LastDiagnostics()
	if diag.FilterDrops[core.FilterDropMinScore] != 2 {
		t.Fatalf("min_score 应丢弃 2 条，实际 %+v", diag.FilterDrops)
	}
}

// ── off-topic 整桶守卫 ─────────────────────────────────────────────────────────

// TestHybridSearch_OffTopicBucketDropped 诱饵引擎整桶只 echo 首词、参照引擎正常
// echo 其余词时，诱饵桶应整桶丢弃并以 off_topic 类型透出。
func TestHybridSearch_OffTopicBucketDropped(t *testing.T) {
	decoy := &mockEngine{name: "decoy", results: []core.SearchResult{
		{Title: "golang tutorial", Url: "http://d1.com", Content: "learn golang basics", Engine: "decoy"},
		{Title: "golang intro", Url: "http://d2.com", Content: "golang for beginners", Engine: "decoy"},
		{Title: "golang guide", Url: "http://d3.com", Content: "golang quickstart", Engine: "decoy"},
	}}
	normal := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "golang release notes", Url: "http://n1.com", Content: "golang release notes and download", Engine: "bing"},
		{Title: "download golang", Url: "http://n2.com", Content: "official release download page", Engine: "bing"},
		{Title: "golang release history", Url: "http://n3.com", Content: "all release notes archive", Engine: "bing"},
	}}
	hs := NewHybridSearch(decoy, normal)
	results, err := hs.SearchRaw("golang release notes download")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range results {
		if r.Engine == "decoy" {
			t.Fatalf("诱饵桶应整桶丢弃，混入结果: %+v", r)
		}
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 normal results, got %d", len(results))
	}
	f := hs.LastDiagnostics().Failures
	if len(f) != 1 || f[0].Engine != "decoy" || f[0].Kind != core.FailureOffTopic {
		t.Fatalf("应以 off_topic 类型透出整桶丢弃，实际 %+v", f)
	}
}

// TestHybridSearch_OffTopicGuardKeepsBucketsWithoutReference 所有桶都只 echo 首词时
// 无参照，无法归因单个引擎，保守放行。
func TestHybridSearch_OffTopicGuardKeepsBucketsWithoutReference(t *testing.T) {
	b1 := &mockEngine{name: "a", results: []core.SearchResult{
		{Title: "golang one", Url: "http://a1.com", Content: "golang", Engine: "a"},
		{Title: "golang two", Url: "http://a2.com", Content: "golang", Engine: "a"},
		{Title: "golang three", Url: "http://a3.com", Content: "golang", Engine: "a"},
	}}
	b2 := &mockEngine{name: "b", results: []core.SearchResult{
		{Title: "golang four", Url: "http://b1.com", Content: "golang", Engine: "b"},
		{Title: "golang five", Url: "http://b2.com", Content: "golang", Engine: "b"},
		{Title: "golang six", Url: "http://b3.com", Content: "golang", Engine: "b"},
	}}
	hs := NewHybridSearch(b1, b2)
	results, err := hs.SearchRaw("golang release notes download")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 6 {
		t.Fatalf("无参照时守卫应放行全部结果，expected 6, got %d", len(results))
	}
	diag := hs.LastDiagnostics()
	if diag.HasFailures() {
		t.Fatalf("无参照时不应产生 off_topic 失败: %+v", diag.Failures)
	}
}

// TestHybridSearch_OffTopicGuardShadowKeepsBucket shadow 模式（默认）：疑似诱饵桶
// 只记 off_topic 失败清单与日志，结果保留不丢弃。
func TestHybridSearch_OffTopicGuardShadowKeepsBucket(t *testing.T) {
	decoy := &mockEngine{name: "decoy", results: []core.SearchResult{
		{Title: "golang tutorial", Url: "http://s1.com", Content: "learn golang basics", Engine: "decoy"},
		{Title: "golang intro", Url: "http://s2.com", Content: "golang for beginners", Engine: "decoy"},
		{Title: "golang guide", Url: "http://s3.com", Content: "golang quickstart", Engine: "decoy"},
	}}
	normal := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "golang release notes", Url: "http://sn1.com", Content: "golang release notes and download", Engine: "bing"},
		{Title: "download golang", Url: "http://sn2.com", Content: "official release download page", Engine: "bing"},
		{Title: "golang release history", Url: "http://sn3.com", Content: "all release notes archive", Engine: "bing"},
	}}
	hs := NewHybridSearch(decoy, normal)
	hs.SetOffTopicGuard(config.OffTopicGuardShadow)
	results, err := hs.SearchRaw("golang release notes download")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 6 {
		t.Fatalf("shadow 模式应保留全部结果（含疑似诱饵桶），expected 6, got %d", len(results))
	}
	foundDecoy := false
	for _, r := range results {
		if r.Engine == "decoy" {
			foundDecoy = true
		}
	}
	if !foundDecoy {
		t.Fatal("shadow 模式诱饵桶结果应保留")
	}
	f := hs.LastDiagnostics().Failures
	if len(f) != 1 || f[0].Engine != "decoy" || f[0].Kind != core.FailureOffTopic {
		t.Fatalf("shadow 应以 off_topic 类型记录疑似整桶，实际 %+v", f)
	}
	if !strings.Contains(f[0].Reason, "未丢弃") {
		t.Fatalf("shadow 失败原因应标明未丢弃: %q", f[0].Reason)
	}
}

// TestHybridSearch_OffTopicGuardOff off 模式完全关闭：不计算不记录。
func TestHybridSearch_OffTopicGuardOff(t *testing.T) {
	decoy := &mockEngine{name: "decoy", results: []core.SearchResult{
		{Title: "golang tutorial", Url: "http://o1.com", Content: "learn golang basics", Engine: "decoy"},
		{Title: "golang intro", Url: "http://o2.com", Content: "golang for beginners", Engine: "decoy"},
		{Title: "golang guide", Url: "http://o3.com", Content: "golang quickstart", Engine: "decoy"},
	}}
	normal := &mockEngine{name: "bing", results: []core.SearchResult{
		{Title: "golang release notes", Url: "http://on1.com", Content: "golang release notes and download", Engine: "bing"},
		{Title: "download golang", Url: "http://on2.com", Content: "official release download page", Engine: "bing"},
		{Title: "golang release history", Url: "http://on3.com", Content: "all release notes archive", Engine: "bing"},
	}}
	hs := NewHybridSearch(decoy, normal)
	hs.SetOffTopicGuard(config.OffTopicGuardOff)
	results, err := hs.SearchRaw("golang release notes download")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 6 {
		t.Fatalf("off 模式应保留全部结果，expected 6, got %d", len(results))
	}
	if d := hs.LastDiagnostics(); d.HasFailures() {
		t.Fatalf("off 模式不应产生 off_topic 记录: %+v", d.Failures)
	}
}

// ── off-topic 守卫辅助函数 ─────────────────────────────────────────────────────

func TestQueryTerms(t *testing.T) {
	terms := queryTerms("golang release notes")
	if len(terms) != 3 || terms[0] != "golang" {
		t.Fatalf("空白分词不符: %v", terms)
	}
	if queryTerms("short") != nil {
		t.Fatal("单词查询应返回 nil（无其余词语义）")
	}
	// CJK 无分隔长查询：2 字滑窗
	if terms := queryTerms("数据库连接池配置"); len(terms) < 2 {
		t.Fatalf("CJK 查询应退化为二元组: %v", terms)
	}
}

func TestEchoRatio(t *testing.T) {
	results := []core.SearchResult{
		{Title: "golang release notes", Content: "download page"},
	}
	r := echoRatio(results, []string{"golang", "release", "missing"})
	if r != 2.0/3.0 {
		t.Fatalf("echoRatio = %v, want %v", r, 2.0/3.0)
	}
	if echoRatio(results, nil) != 1 {
		t.Fatal("空词项集合回声率应为 1")
	}
}
