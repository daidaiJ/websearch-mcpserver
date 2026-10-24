package enhance

import (
	"math"
	"testing"
	"websearch/pkg/search/core"
)

// ── 信号函数 ─────────────────────────────────────────────────────────────────

func TestCiteFactor(t *testing.T) {
	cases := []struct {
		citedBy int
		want    float64
	}{
		{0, 1.00},
		{10, 1.17},
		{100, 1.33},
		{1000, 1.50},
		{10000, 1.66},
	}
	for _, c := range cases {
		got := CiteFactor(c.citedBy)
		if math.Abs(got-c.want) > 0.02 {
			t.Errorf("CiteFactor(%d) = %v, 期望 ≈%v", c.citedBy, got, c.want)
		}
	}
	// clamp 上限 1.7
	if got := CiteFactor(1 << 30); got > 1.7 {
		t.Errorf("CiteFactor 应 clamp 到 1.7, got %v", got)
	}
	// 单调递增
	prev := 0.0
	for i := 0; i < 10000; i += 100 {
		got := CiteFactor(i)
		if got < prev {
			t.Errorf("CiteFactor 应单调递增: %d -> %v < %v", i, got, prev)
		}
		prev = got
	}
}

func TestJournalBoost(t *testing.T) {
	cases := []struct {
		journal string
		want    float64
	}{
		{"Nature", 0.08},
		{"Science", 0.08},
		{"Cell", 0.07},
		{"The Lancet", 0.07},
		// Contains 匹配：全称含 "science" → 命中 0.08 条目（高于 "pnas" 的 0.05）
		{"Proceedings of the National Academy of Sciences (PNAS)", 0.08},
		// 含 "nature" → 命中 0.08 条目（高于 "nature communications" 的 0.05）
		{"Nature Communications", 0.08},
		{"Advances in Neural Information Processing Systems (NeurIPS)", 0.05},
		{"ACM SIGIR Conference on Research and Development in Information Retrieval", 0.04},
		{"IEEE Transactions on Pattern Analysis and Machine Intelligence", 0.0},
		{"", 0.0},
	}
	for _, c := range cases {
		got := JournalBoost(c.journal)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("JournalBoost(%q) = %v, 期望 %v", c.journal, got, c.want)
		}
	}
	// 大小写不敏感
	if got := JournalBoost("nature"); got != 0.08 {
		t.Errorf("JournalBoost 应大小写不敏感, got %v", got)
	}
}

func TestAcademicRecencyFactor(t *testing.T) {
	// 非时间敏感始终 1.0
	if f := AcademicRecencyFactor(false, "2020-01-01"); f != 1.0 {
		t.Errorf("非时间敏感应返回 1.0, got %v", f)
	}
	// 无有效日期返回 1.0
	if f := AcademicRecencyFactor(true, ""); f != 1.0 {
		t.Errorf("空日期应返回 1.0, got %v", f)
	}
	// 近期（<1年）应 >1.0
	if f := AcademicRecencyFactor(true, "2026-07-01"); f != 1.15 {
		t.Errorf("近一年应 ×1.15, got %v", f)
	}
	// 远期（≥5年）应 1.0
	if f := AcademicRecencyFactor(true, "2020-01-01"); f != 1.0 {
		t.Errorf("≥5年应 ×1.0, got %v", f)
	}
}

// ── 评分流水线 ───────────────────────────────────────────────────────────────

// TestEnhanceAcademicResults 高引 + 期刊 + PDF 信号的论文应排前，低质结果被过滤。
func TestEnhanceAcademicResults(t *testing.T) {
	buckets := []core.ScoreBucket{
		{Name: "openalex", Weight: 1.0, Results: []core.SearchResult{
			{Title: "Attention Is All You Need", Url: "https://arxiv.org/abs/1706.03762",
				Content: "transformer attention mechanism", Journal: "arXiv", CitedBy: 100000, PDFURL: "https://arxiv.org/pdf/1706.03762", DOI: "10.48550/arXiv.1706.03762"},
			{Title: "Low Quality Paper", Url: "https://example.com/low", Content: "unrelated noise", Journal: "Unknown Venue", CitedBy: 0},
		}},
		{Name: "crossref", Weight: 1.0, Results: []core.SearchResult{
			{Title: "Attention Is All You Need", Url: "https://arxiv.org/abs/1706.03762",
				Content: "transformer attention", Journal: "arXiv", CitedBy: 90000},
			{Title: "Nature High Impact", Url: "https://nature.com/articles/xyz",
				Content: "important discovery", Journal: "Nature", CitedBy: 500, PDFURL: "https://nature.com/pdf"},
		}},
	}
	got := EnhanceAcademicResults("transformer attention mechanism", buckets, 0.02, 10)
	if len(got) == 0 {
		t.Fatal("学术流水线不应返回空")
	}
	// Nature 期刊 + 500 引用 + PDF 全文的信号加成最高 → 第一
	if got[0].Url != "https://nature.com/articles/xyz" {
		t.Errorf("Nature 高引论文应排第一, got %q", got[0].Url)
	}
	// 双引擎共识 + 10 万引用的 Attention 论文第二，并记录两个来源引擎
	if len(got) < 2 || got[1].Url != "https://arxiv.org/abs/1706.03762" {
		t.Errorf("Attention 论文应排第二, got %+v", urlsOf(got))
	}
	if len(got[1].Engines) != 2 {
		t.Errorf("应记录 2 个引擎, got %v", got[1].Engines)
	}
	// 低质量论文（无引用/无期刊/无PDF）应被过滤
	for _, r := range got {
		if r.Url == "https://example.com/low" {
			t.Errorf("低质量论文应被过滤: %+v", r)
		}
	}
	// Nature 高引论文应保留
	foundNature := false
	for _, r := range got {
		if r.Url == "https://nature.com/articles/xyz" {
			foundNature = true
		}
	}
	if !foundNature {
		t.Error("Nature 高引论文应保留")
	}
}

// TestEnhanceAcademicScoreOrder 结果应按 Score 降序。
func TestEnhanceAcademicScoreOrder(t *testing.T) {
	buckets := []core.ScoreBucket{
		{Name: "openalex", Results: []core.SearchResult{
			{Title: "A", Url: "https://a.com", Content: "same topic", CitedBy: 10},
			{Title: "B", Url: "https://b.com", Content: "same topic", CitedBy: 100},
			{Title: "C", Url: "https://c.com", Content: "same topic", CitedBy: 5},
		}},
	}
	got := EnhanceAcademicResults("same topic", buckets, 0.0, 10)
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Errorf("结果未按 Score 降序: [%d]=%v < [%d]=%v", i-1, got[i-1].Score, i, got[i].Score)
		}
	}
	// 高引的 B 应排第一
	if got[0].Url != "https://b.com" {
		t.Errorf("高引论文应排第一, got %q", got[0].Url)
	}
}

// TestEnhanceAcademicDOIMerge 同 DOI 不同 URL 的跨源结果应按 DOI 合并为一条，
// 元数据择优（CitedBy 取较大值），Engines 记录两个来源。
func TestEnhanceAcademicDOIMerge(t *testing.T) {
	buckets := []core.ScoreBucket{
		{Name: "openalex", Results: []core.SearchResult{
			{Title: "Attention Is All You Need", Url: "https://openalex.org/W1",
				Content: "openalex abstract", DOI: "10.48550/arXiv.1706.03762", CitedBy: 90000},
		}},
		{Name: "crossref", Results: []core.SearchResult{
			{Title: "Attention Is All You Need", Url: "https://doi.org/10.48550/arxiv.1706.03762",
				Content: "crossref abstract longer content here", DOI: "https://DOI.org/10.48550/arXiv.1706.03762", CitedBy: 95000},
			{Title: "Unrelated No-DOI", Url: "https://example.com/x", Content: "no doi here"},
		}},
	}
	got := EnhanceAcademicResults("attention mechanism", buckets, 0.001, 10)

	// 同 DOI（大小写/前缀差异归一后相同）+ 无 DOI 的独立条目 = 2 条
	if len(got) != 2 {
		t.Fatalf("同 DOI 应合并为 1 条 + 无 DOI 独立 1 条, got %d: %+v", len(got), urlsOf(got))
	}
	var merged *core.SearchResult
	for i := range got {
		if got[i].DOI != "" {
			merged = &got[i]
		}
	}
	if merged == nil {
		t.Fatal("应存在合并后的 DOI 结果")
	}
	if len(merged.Engines) != 2 {
		t.Errorf("合并结果应记录 2 个引擎, got %v", merged.Engines)
	}
	if merged.CitedBy != 95000 {
		t.Errorf("CitedBy 应取较大值 95000, got %d", merged.CitedBy)
	}
	if merged.Content != "crossref abstract longer content here" {
		t.Errorf("合并应保留更长的摘要, got %q", merged.Content)
	}
}

// TestEnhanceAcademicNoDOIURLKey 无 DOI 结果行为与现状一致（URL 键去重）。
func TestEnhanceAcademicNoDOIURLKey(t *testing.T) {
	buckets := []core.ScoreBucket{
		{Name: "a", Results: []core.SearchResult{
			{Title: "X", Url: "https://example.com/paper", Content: "x"},
		}},
		{Name: "b", Results: []core.SearchResult{
			// URL 归一化后相同（末尾斜杠差异）→ 合并
			{Title: "X", Url: "https://example.com/paper/", Content: "x longer content"},
		}},
	}
	got := EnhanceAcademicResults("topic", buckets, 0.0, 10)
	if len(got) != 1 {
		t.Fatalf("同 URL 应合并为 1 条, got %d", len(got))
	}
	if len(got[0].Engines) != 2 {
		t.Errorf("应记录 2 个引擎, got %v", got[0].Engines)
	}
}

// TestApplyAcademicScoreFloorPerEngineTop 每引擎保底：引擎内第一名低于阀值也保留。
func TestApplyAcademicScoreFloorPerEngineTop(t *testing.T) {
	scored := []core.SearchResult{
		{Url: "https://top1.com", Score: 0.5},
		{Url: "https://a.com", Score: 0.05},
		{Url: "https://b.com", Score: 0.001}, // 低分但可能是某引擎第一名
		{Url: "https://c.com", Score: 0.0005},
	}
	buckets := []core.ScoreBucket{
		{Name: "engineA", Results: []core.SearchResult{{Url: "https://b.com"}}},
	}
	kept := applyAcademicScoreFloor(scored, buckets, 0.02)

	keptURLs := make(map[string]bool)
	for _, r := range kept {
		keptURLs[r.Url] = true
	}
	// 引擎 A 的第一名 b.com 应被保底恢复
	if !keptURLs["https://b.com"] {
		t.Errorf("每引擎保底应恢复 b.com, got %v", keptURLs)
	}
	// 无保底的低分 c.com 应被过滤
	if keptURLs["https://c.com"] {
		t.Errorf("低分 c.com 应被过滤, got %v", keptURLs)
	}
	// 保底后仍按 Score 降序
	for i := 1; i < len(kept); i++ {
		if kept[i-1].Score < kept[i].Score {
			t.Errorf("保底后应保持 Score 降序: %+v", kept)
		}
	}
}

// TestEnhanceAcademicRecencyBoost 时间敏感查询时近期论文加分提升排名。
func TestEnhanceAcademicRecencyBoost(t *testing.T) {
	buckets := []core.ScoreBucket{
		{Name: "openalex", Results: []core.SearchResult{
			{Title: "Old Paper", Url: "https://old.com", Content: "latest research topic", CitedBy: 1000, PublishDate: "2015-06-01"},
			{Title: "New Paper", Url: "https://new.com", Content: "latest research topic", CitedBy: 800, PublishDate: "2026-07-01"},
		}},
	}
	// 含近期意图词（2026）→ 新鲜度因子生效
	got := EnhanceAcademicResults("latest 2026 research", buckets, 0.0, 10)
	if got[0].Url != "https://new.com" {
		t.Errorf("时间敏感查询下近期论文应排第一, got %q", got[0].Url)
	}
	// 普通查询 → 新鲜度不生效，高引老论文排第一
	got2 := EnhanceAcademicResults("research topic", buckets, 0.0, 10)
	if got2[0].Url != "https://old.com" {
		t.Errorf("非时间敏感查询下高引论文应排第一, got %q", got2[0].Url)
	}
}
