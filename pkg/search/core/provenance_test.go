package core

import (
	"strings"
	"testing"
	"time"
)

// ── DateSourceState 三态规范化 ──────────────────────────────────────────────

func TestDateSourceState(t *testing.T) {
	cases := []struct {
		name string
		r    SearchResult
		want string
	}{
		{"无日期", SearchResult{}, DateSourceUndated},
		{"有日期有结构化注记", SearchResult{PublishDate: "2024-01-02", DateSource: DateSourceStructured}, DateSourceStructured},
		{"有日期有摘要注记", SearchResult{PublishDate: "2024-01-02", DateSource: DateSourceSnippet}, DateSourceSnippet},
		{"有日期无注记（旧缓存行）→ 弱口径", SearchResult{PublishDate: "2024-01-02"}, DateSourceSnippet},
		{"空白日期", SearchResult{PublishDate: "  "}, DateSourceUndated},
		{"有日期未知注记 → 弱口径", SearchResult{PublishDate: "2024-01-02", DateSource: "weird"}, DateSourceSnippet},
	}
	for _, c := range cases {
		if got := DateSourceState(c.r); got != c.want {
			t.Errorf("%s: DateSourceState = %q, want %q", c.name, got, c.want)
		}
	}
}

// ── PreferDate 跨引擎日期采信 ───────────────────────────────────────────────

func TestPreferDate(t *testing.T) {
	t.Run("结构化覆盖摘要", func(t *testing.T) {
		dst := SearchResult{PublishDate: "2024-01-01", DateSource: DateSourceSnippet}
		PreferDate(&dst, SearchResult{PublishDate: "2024-02-02", DateSource: DateSourceStructured})
		if dst.PublishDate != "2024-02-02" || dst.DateSource != DateSourceStructured {
			t.Errorf("弱日期应被强日期覆盖: %+v", dst)
		}
	})
	t.Run("补齐缺失日期", func(t *testing.T) {
		dst := SearchResult{}
		PreferDate(&dst, SearchResult{PublishDate: "2024-02-02", DateSource: DateSourceSnippet})
		if dst.PublishDate != "2024-02-02" {
			t.Errorf("缺失日期应补齐: %+v", dst)
		}
	})
	t.Run("结构化不被摘要降级", func(t *testing.T) {
		dst := SearchResult{PublishDate: "2024-01-01", DateSource: DateSourceStructured}
		PreferDate(&dst, SearchResult{PublishDate: "2024-02-02", DateSource: DateSourceSnippet})
		if dst.PublishDate != "2024-01-01" || dst.DateSource != DateSourceStructured {
			t.Errorf("强日期不应被弱日期覆盖: %+v", dst)
		}
	})
	t.Run("同档位先到先得", func(t *testing.T) {
		dst := SearchResult{PublishDate: "2024-01-01", DateSource: DateSourceSnippet}
		PreferDate(&dst, SearchResult{PublishDate: "2024-02-02", DateSource: DateSourceSnippet})
		if dst.PublishDate != "2024-01-01" {
			t.Errorf("同档位应保留先到日期: %+v", dst)
		}
	})
	t.Run("空来源不改动", func(t *testing.T) {
		dst := SearchResult{PublishDate: "2024-01-01", DateSource: DateSourceSnippet}
		PreferDate(&dst, SearchResult{})
		if dst.PublishDate != "2024-01-01" {
			t.Errorf("src 无日期不应改动 dst: %+v", dst)
		}
	})
}

// ── FormatDateSource 渲染 ───────────────────────────────────────────────────

func TestFormatDateSource(t *testing.T) {
	if got := FormatDateSource("", DateSourceStructured); got != "" {
		t.Errorf("无日期应渲染为空, got %q", got)
	}
	if got := FormatDateSource("2024-01-02", DateSourceSnippet); !strings.Contains(got, "snippet") || !strings.Contains(got, "弱") {
		t.Errorf("snippet 应带弱标注, got %q", got)
	}
	if got := FormatDateSource("2024-01-02", DateSourceStructured); !strings.Contains(got, "structured") {
		t.Errorf("structured 应带来源标注, got %q", got)
	}
	if got := FormatDateSource("2024-01-02", ""); !strings.Contains(got, "snippet") {
		t.Errorf("旧缓存行应按弱口径渲染, got %q", got)
	}
}

// ── ProvenanceHeader 响应溯源头 ─────────────────────────────────────────────

func TestProvenanceHeader(t *testing.T) {
	if got := ProvenanceHeader(time.Time{}, 0); got != "" {
		t.Errorf("零值时间应返回空串, got %q", got)
	}

	retrieved := time.Date(2026, 10, 6, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	fresh := ProvenanceHeader(retrieved, 0)
	if !strings.Contains(fresh, "retrieved_at: 2026-10-06T12:00:00+08:00") {
		t.Errorf("实时响应应含 retrieved_at: %q", fresh)
	}
	if strings.Contains(fresh, "cache_age_seconds") {
		t.Errorf("实时响应不应含 cache_age_seconds: %q", fresh)
	}
	if !strings.Contains(fresh, "usage_note") {
		t.Errorf("响应头应含固定 usage_note: %q", fresh)
	}

	cached := ProvenanceHeader(retrieved, 2*time.Hour)
	if !strings.Contains(cached, "cache_age_seconds: 7200") {
		t.Errorf("缓存响应应含 cache_age_seconds: %q", cached)
	}
}
