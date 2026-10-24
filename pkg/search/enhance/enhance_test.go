package enhance

import (
	"math"
	"testing"
	"websearch/pkg/search/core"
)

// ── 文本信号 ────────────────────────────────────────────────────────────────

func TestLexicalAlignment(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		title   string
		snippet string
		wantMin float64
		wantMax float64
	}{
		{"完全命中", "go generics performance", "Go Generics Performance Guide", "benchmark of go generics performance", 1.0, 1.0},
		{"部分命中", "go generics performance", "Go Tutorial", "learn go basics", 0.0, 0.4},
		{"完全不相关", "pgvector hnsw index", "cooking recipes", "how to bake bread", 0.0, 0.0},
		{"中文命中", "泛型 性能", "Go 泛型性能测试", "泛型 性能 基准", 0.5, 1.0},
		{"空查询", "", "anything", "anything", 0.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LexicalAlignment(tt.query, tt.title, tt.snippet)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("LexicalAlignment(%q) = %v, 期望在 [%v, %v]", tt.query, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestRareTermsFactor(t *testing.T) {
	// 复合词命中应提升 (>1.0)
	hit := RareTermsFactor("pgvector hnsw", "pgvector HNSW index in postgres", "using pgvector for vector search")
	if hit <= 1.0 {
		t.Errorf("复合词命中应 >1.0, got %v", hit)
	}
	// 复合词全缺失应降低 (=0.5)
	miss := RareTermsFactor("fts5_virtual", "generic article about databases", "no relevant term here")
	if miss > 0.6 {
		t.Errorf("复合词全缺失应接近 0.5, got %v", miss)
	}
	// 因子应始终落在 [0.5, 1.6]
	for _, f := range []float64{hit, miss} {
		if f < 0.5 || f > 1.6 {
			t.Errorf("因子越界: %v 不在 [0.5, 1.6]", f)
		}
	}
}

// ── 域名信号 ────────────────────────────────────────────────────────────────

func TestDomainQuality(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		query         string
		hasErrorToken bool
		want          float64
	}{
		{"普通站点默认 1.0", "https://example.com/article", "some query", false, 1.0},
		{"品牌电商惩罚", "https://www.amazon.com/dp/B01", "postgres tutorial", false, brandPenalty},
		{"商业 TLD 惩罚", "https://cool.shop/item", "postgres tutorial", false, brandPenalty},
		{"低质量 TLD 惩罚", "https://spam.tk/page", "postgres tutorial", false, lowQualityTLDPenalty},
		{"词典站错误码惩罚", "https://www.merriam-webster.com/x", "ECONNREFUSED", true, dictionaryErrorPenalty},
		{"词典站非错误码不惩罚", "https://www.merriam-webster.com/x", "define serendipity", false, 1.0},
		{"MDN 元素漂移惩罚", "https://developer.mozilla.org/web/html/element/table", "postgres index", false, mdnElementDriftPenalty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DomainQuality(tt.url, tt.query, tt.hasErrorToken)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("DomainQuality(%q) = %v, 期望 %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestAuthorityBoost(t *testing.T) {
	// 已知主题精确匹配
	if b := AuthorityBoost("redis persistence", "https://redis.io/docs", 1.0); b < 0.19 {
		t.Errorf("redis.io 精确匹配应 >=0.20, got %v", b)
	}
	// 官方文档站
	if b := AuthorityBoost("python asyncio", "https://docs.python.org/3/library/asyncio.html", 1.0); b < 0.17 {
		t.Errorf("docs.python.org 应 >=0.18, got %v", b)
	}
	// 未知站点无加分
	if b := AuthorityBoost("random query", "https://random-blog.net/post", 1.0); b != 0 {
		t.Errorf("未知站点应无加分, got %v", b)
	}
	// 稀有词全缺失时削弱
	full := AuthorityBoost("redis persistence", "https://redis.io/docs", 1.0)
	weak := AuthorityBoost("redis persistence", "https://redis.io/docs", 0.5)
	if weak >= full {
		t.Errorf("稀有词缺失应削弱权威加分: weak=%v full=%v", weak, full)
	}
}

// ── 融合 / 共识 ─────────────────────────────────────────────────────────────

func TestRRFScore(t *testing.T) {
	// 排名越靠前分数越高
	top := RRFScore(map[string]int{"bing": 0}, 60)
	low := RRFScore(map[string]int{"bing": 10}, 60)
	if top <= low {
		t.Errorf("靠前排名应得分更高: top=%v low=%v", top, low)
	}
	// 多引擎命中累加
	multi := RRFScore(map[string]int{"bing": 0, "google": 0}, 60)
	single := RRFScore(map[string]int{"bing": 0}, 60)
	if multi <= single {
		t.Errorf("多引擎命中应累加: multi=%v single=%v", multi, single)
	}
	// K<=0 使用默认
	if RRFScore(map[string]int{"x": 0}, 0) != RRFScore(map[string]int{"x": 0}, rrfK) {
		t.Error("K<=0 应回退默认 rrfK")
	}
}

func TestConsensusBoost(t *testing.T) {
	cases := map[int]float64{1: 0, 2: 0.05, 3: 0.10, 4: 0.12, 5: 0.12}
	for n, want := range cases {
		if got := ConsensusBoost(n); math.Abs(got-want) > 1e-9 {
			t.Errorf("ConsensusBoost(%d) = %v, 期望 %v", n, got, want)
		}
	}
}

func TestNormalizeURLKey(t *testing.T) {
	a := normalizeURLKey("https://www.example.com/path/")
	b := normalizeURLKey("http://example.com/path")
	if a != b {
		t.Errorf("scheme/www/末尾斜杠应归一化相同: %q vs %q", a, b)
	}
}

// ── 意图信号 ────────────────────────────────────────────────────────────────

func TestIntentSignals(t *testing.T) {
	if !HasTemporalIntent("latest go release") {
		t.Error("含 latest 应判为时间敏感")
	}
	if HasTemporalIntent("go generics guide") {
		t.Error("普通查询不应判为时间敏感")
	}
	if !HasErrorToken("ECONNREFUSED connection failed") {
		t.Error("错误码应被识别")
	}
	if !HasErrorToken("NullPointerException in java") {
		t.Error("异常类名应被识别")
	}
	if got := classifyIntent("arxiv paper on transformers"); got != "papers" {
		t.Errorf("应分类为 papers, got %q", got)
	}
	if got := classifyIntent("python how to tutorial"); got != "docs" {
		t.Errorf("应分类为 docs, got %q", got)
	}
}

func TestRecencyFactor(t *testing.T) {
	// 非时间敏感始终 1.0
	if RecencyFactor(false, "2020-01-01") != 1.0 {
		t.Error("非时间敏感应返回 1.0")
	}
	// 无有效日期返回 1.0
	if RecencyFactor(true, "") != 1.0 {
		t.Error("空日期应返回 1.0")
	}
	if RecencyFactor(true, "garbage") != 1.0 {
		t.Error("无法解析日期应返回 1.0")
	}
}

// ── 阀值过滤 ────────────────────────────────────────────────────────────────

func TestApplyScoreFloor(t *testing.T) {
	results := []core.SearchResult{
		{Url: "https://a.com", Score: 0.5, Engine: "bing"},    // Top-1
		{Url: "https://b.com", Score: 0.20, Engine: "bing"},   // 高于阀值 → 保留
		{Url: "https://c.com", Score: 0.02, Engine: "google"}, // 低于阀值 → 被过滤（无每引擎保底）
	}
	kept := ApplyScoreFloor(results, 0.05)
	// Top-1 必留
	if len(kept) == 0 || kept[0].Url != "https://a.com" {
		t.Fatalf("Top-1 应保留, got %+v", kept)
	}
	// 低于阀值的 c.com 应被过滤（不再有每引擎保底）
	for _, r := range kept {
		if r.Url == "https://c.com" {
			t.Errorf("低于阀值的结果 c.com 应被过滤, got kept=%+v", kept)
		}
	}
	// 高于阀值的 b.com 应保留
	foundB := false
	for _, r := range kept {
		if r.Url == "https://b.com" {
			foundB = true
		}
	}
	if !foundB {
		t.Error("高于阀值的 b.com 应保留")
	}
}

func TestApplyScoreFloorMinTwo(t *testing.T) {
	results := []core.SearchResult{
		{Url: "https://a.com", Score: 0.5, Engine: "bing"},
		{Url: "https://b.com", Score: 0.001, Engine: "bing"},
	}
	kept := ApplyScoreFloor(results, 0.05)
	if len(kept) < 2 {
		t.Errorf("原结果 >=2 时应至少返回 2 条, got %d", len(kept))
	}
}

// ── 完整流水线 ──────────────────────────────────────────────────────────────

func TestEnhanceResults(t *testing.T) {
	buckets := []core.ScoreBucket{
		{Name: "bing", Weight: 1.0, Results: []core.SearchResult{
			{Title: "Go Generics Performance", Url: "https://go.dev/blog/generics", Content: "go generics performance benchmark", Engine: "bing"},
			{Title: "Amazon Deals", Url: "https://www.amazon.com/dp/x", Content: "buy now", Engine: "bing"},
		}},
		{Name: "google", Weight: 1.0, Results: []core.SearchResult{
			{Title: "Go Generics Performance", Url: "https://go.dev/blog/generics", Content: "go generics performance guide", Engine: "google"},
			{Title: "Unrelated", Url: "https://spam.tk/x", Content: "random", Engine: "google"},
		}},
	}
	got := EnhanceResults("go generics performance", buckets, 0.05, 10)
	if len(got) == 0 {
		t.Fatal("流水线不应返回空")
	}
	// 双引擎共识 + 高词汇对齐的 go.dev 应排第一
	if got[0].Url != "https://go.dev/blog/generics" {
		t.Errorf("共识高相关结果应排第一, got %q", got[0].Url)
	}
	// 该结果应记录两个来源引擎
	if len(got[0].Engines) != 2 {
		t.Errorf("共识结果应记录 2 个引擎, got %v", got[0].Engines)
	}
	// 结果应按 Score 降序
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Errorf("结果未按 Score 降序: [%d]=%v < [%d]=%v", i-1, got[i-1].Score, i, got[i].Score)
		}
	}
}

func TestEnhanceResultsMaxSize(t *testing.T) {
	// 构造 8 个双引擎共识 + 高词汇对齐的结果，确保均超过阀值，以真正验证 maxSize 截断。
	var bing, google []core.SearchResult
	for i := 0; i < 8; i++ {
		u := "https://example.com/database-index-tuning-" + string(rune('a'+i))
		bing = append(bing, core.SearchResult{Title: "database index tuning", Url: u, Content: "database index tuning guide", Engine: "bing"})
		google = append(google, core.SearchResult{Title: "database index tuning", Url: u, Content: "database index tuning guide", Engine: "google"})
	}
	buckets := []core.ScoreBucket{
		{Name: "bing", Weight: 1.0, Results: bing},
		{Name: "google", Weight: 1.0, Results: google},
	}
	got := EnhanceResults("database index tuning", buckets, 0.05, 5)
	if len(got) != 5 {
		t.Errorf("均超阀值的结果应按 maxSize=5 截断, got %d", len(got))
	}
}

// TestEnhanceFiltersLowQuality 观测性测试：实测低质量内容（品牌电商/低质 TLD/离题）被确实裁剪。
func TestEnhanceFiltersLowQuality(t *testing.T) {
	query := "kubernetes pod networking"
	buckets := []core.ScoreBucket{
		{Name: "bing", Weight: 1.0, Results: []core.SearchResult{
			// 双引擎共识 + 权威域名，应保留
			{Title: "Kubernetes Pod Networking Concepts", Url: "https://kubernetes.io/docs/concepts/services-networking/", Content: "kubernetes pod networking model", Engine: "bing"},
			// 品牌电商域名，dq=0.2，应被过滤
			{Title: "Buy Kubernetes Book", Url: "https://www.amazon.com/dp/k8s", Content: "buy now cheap", Engine: "bing"},
			// 普通单引擎博客，无 Boost，应被过滤
			{Title: "K8s networking tips", Url: "https://randomblog.example/k8s", Content: "kubernetes pod networking tips", Engine: "bing"},
			// 低质 TLD + 离题，应被过滤
			{Title: "Cheap ads", Url: "https://spam.tk/ads", Content: "unrelated advertising content", Engine: "bing"},
		}},
		{Name: "google", Weight: 1.0, Results: []core.SearchResult{
			{Title: "Kubernetes Pod Networking Concepts", Url: "https://kubernetes.io/docs/concepts/services-networking/", Content: "kubernetes pod networking", Engine: "google"},
			{Title: "Medium guide", Url: "https://medium.com/@x/k8s-net", Content: "kubernetes pod networking guide", Engine: "google"},
		}},
	}

	totalUnique := 5 // kubernetes.io, amazon, randomblog, spam.tk, medium
	got := EnhanceResults(query, buckets, 0.05, 10)

	t.Logf("去重后=%d 过滤后=%d", totalUnique, len(got))
	for _, r := range got {
		t.Logf("KEPT  score=%.4f engines=%v %s", r.Score, r.Engines, r.Url)
	}

	// 确实发生了裁剪
	if len(got) >= totalUnique {
		t.Errorf("期望过滤掉低质量结果，但去重后=%d 过滤后=%d 未减少", totalUnique, len(got))
	}

	keptURLs := make(map[string]bool)
	for _, r := range got {
		keptURLs[r.Url] = true
	}
	// 权威 + 共识结果必须保留
	if !keptURLs["https://kubernetes.io/docs/concepts/services-networking/"] {
		t.Error("权威共识结果 kubernetes.io 应被保留")
	}
	// 低质量结果必须被过滤
	for _, bad := range []string{"https://www.amazon.com/dp/k8s", "https://spam.tk/ads"} {
		if keptURLs[bad] {
			t.Errorf("低质量结果应被过滤: %s", bad)
		}
	}
}
