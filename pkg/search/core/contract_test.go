package core

import (
	"strings"
	"testing"
	"time"
)

// 本文件是 MCP 输出契约测试（2026-10-24 拍板 N5）：固化格式层字段顺序、
// 「空结果是合法结果不是错误」与易变字段位置约定，防止无意的格式漂移
// 打穿客户端 prompt/KV cache。改输出格式必须同步更新本文件并说明理由。

// TestFormatMDFieldOrder 固化结果条目的字段顺序：标题 → url →（可选 meta 行）→ 内容。
// 内容行必须始终是最后一段，任何新增字段只能插在内容行之前。
func TestFormatMDFieldOrder(t *testing.T) {
	s := FormatMD(1, "标题", "https://example.com", "正文")
	order := []string{"**标题**: 标题", "**url**: https://example.com", "**内容**: 正文"}
	pos := -1
	for _, field := range order {
		next := strings.Index(s, field)
		if next < 0 || next < pos {
			t.Fatalf("field %q out of order in %q", field, s)
		}
		pos = next
	}
	if !strings.HasSuffix(s, "**内容**: 正文  \n") {
		t.Fatalf("content line must be last, got %q", s)
	}
}

func TestFormatMDScoreFieldOrder(t *testing.T) {
	s := FormatMDScore(2, "标题", "https://example.com", "tinyfish_api", "0.85",
		FormatDateSource("2026-10-01", DateSourceStructured), "正文")
	order := []string{
		"## 结果 2", "**标题**: 标题", "**url**: https://example.com",
		"**来源**: tinyfish_api", "**相关性**: 0.85", "**日期**: 2026-10-01（structured）", "**内容**: 正文",
	}
	pos := -1
	for _, field := range order {
		next := strings.Index(s, field)
		if next < 0 || next < pos {
			t.Fatalf("field %q out of order in %q", field, s)
		}
		pos = next
	}
	// 空 meta 行不占位：engine/score/date 为空时输出与无 meta 版本字段序列一致
	plain := FormatMDScore(2, "标题", "https://example.com", "", "", "", "正文")
	for _, field := range []string{"**来源**", "**相关性**", "**日期**"} {
		if strings.Contains(plain, field) {
			t.Fatalf("empty %q must be omitted, got %q", field, plain)
		}
	}
}

// TestFormatDateSourceEmptyIsLegal 空日期是合法输入而非错误：返回空串隐藏该行。
func TestFormatDateSourceEmptyIsLegal(t *testing.T) {
	if got := FormatDateSource("", ""); got != "" {
		t.Fatalf("undated = %q, want empty", got)
	}
}

// TestMDSearchHeaderStablePrefix 搜索头是稳定前缀：查询与结果数之外不得混入易变字段
//（时间戳/请求 id 等只能进 ProvenanceHeader，避免逐请求打穿客户端 KV cache）。
func TestMDSearchHeaderStablePrefix(t *testing.T) {
	a := MDSearchHeader("q", 3)
	b := MDSearchHeader("q", 3)
	if a != b {
		t.Fatal("header must be deterministic for identical input")
	}
	if strings.ContainsAny(a, "TZ") && strings.Contains(a, "retrieved") {
		t.Fatalf("header must not embed timestamps: %q", a)
	}
}

// TestProvenanceHeaderVolatileFields 易变字段（检索时间/缓存年龄）集中在
// ProvenanceHeader，且固定为「时间在前、注记在后」的两行结构；零值时间返回空串。
func TestProvenanceHeaderVolatileFields(t *testing.T) {
	if got := ProvenanceHeader(time.Time{}, 0); got != "" {
		t.Fatalf("zero retrievedAt must yield empty header, got %q", got)
	}
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	fresh := ProvenanceHeader(at, 0)
	cached := ProvenanceHeader(at, 90*time.Second)
	if !strings.HasPrefix(fresh, "> 🕒 retrieved_at: 2026-10-24T12:00:00Z\n") {
		t.Fatalf("fresh header = %q", fresh)
	}
	if !strings.Contains(cached, "cache_age_seconds: 90") {
		t.Fatalf("cached header = %q", cached)
	}
	if strings.Contains(fresh, "cache_age") {
		t.Fatal("cache_age_seconds must appear only on cache hits")
	}
	if !strings.HasSuffix(fresh, "核实。\n") {
		t.Fatal("usage_note must be the trailing fixed line")
	}
}

// TestFormatScoreDeterminism 分数渲染确定；score<=0 返回空串（隐藏该行），
// 与 FormatMDScore 的「空 meta 不占位」约定衔接。
func TestFormatScoreDeterminism(t *testing.T) {
	if FormatScore(0.5) != FormatScore(0.5) {
		t.Fatal("score formatting must be deterministic")
	}
	if got := FormatScore(0); got != "" {
		t.Fatalf("zero score must render empty (line hidden), got %q", got)
	}
	if got := FormatScore(0.5); got != "0.5000" {
		t.Fatalf("score = %q, want 0.5000", got)
	}
}
