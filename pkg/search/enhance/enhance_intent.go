package enhance

import (
	"regexp"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// 意图信号：意图分类、时间敏感检测、错误码检测、新鲜度 Boost (Recency Boost)
// 参考 docs/native-engine-optimization.md 模块七、模块九。
// ──────────────────────────────────────────────────────────────────────────────

var (
	recencyIntentRe = regexp.MustCompile(`(?i)(recent|latest|newest|just released|today|this week|breaking|news|更新|最新|最近|今年|近期|新版)`)
	yearRe          = regexp.MustCompile(`\b(20\d{2})\b`)

	// 错误码 / 异常类名检测
	errorNoRe        = regexp.MustCompile(`\bE[A-Z]{3,}\b`)
	underscoreCodeRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]*_[A-Z0-9_]+\b`)
	codeWithNumRe    = regexp.MustCompile(`\b[A-Z]{3,}\s+\d{2,}\b`)
	errorClassRe     = regexp.MustCompile(`\b[A-Z][a-z]+(?:[A-Z][a-z]+)*(Error|Exception|Warning)\b`)

	papersRe = regexp.MustCompile(`(?i)(arxiv|paper|citation|\bdoi\b|preprint|论文|文献)`)
	codeRe   = regexp.MustCompile(`(?i)(error|exception|traceback|stack ?trace|panic|报错|异常|栈)`)
	docsRe   = regexp.MustCompile(`(?i)(how to|tutorial|reference|documentation|docs|guide|教程|文档|指南|用法)`)
)

// HasTemporalIntent 判断 query 是否时间敏感（含"最新/latest/年份"等）。
func HasTemporalIntent(query string) bool {
	if recencyIntentRe.MatchString(query) {
		return true
	}
	// 含近两年年份视为时间敏感
	if m := yearRe.FindString(query); m != "" {
		if y, err := time.Parse("2006", m); err == nil {
			if time.Since(y).Hours()/24/365 <= 2 {
				return true
			}
		}
	}
	return false
}

// HasErrorToken 判断 query 是否包含错误码/异常类名。
func HasErrorToken(query string) bool {
	return errorNoRe.MatchString(query) ||
		underscoreCodeRe.MatchString(query) ||
		codeWithNumRe.MatchString(query) ||
		errorClassRe.MatchString(query)
}

// classifyIntent 粗粒度意图分类：papers / code / docs / news / general。
func classifyIntent(query string) string {
	switch {
	case papersRe.MatchString(query):
		return "papers"
	case codeRe.MatchString(query) || HasErrorToken(query):
		return "code"
	case docsRe.MatchString(query):
		return "docs"
	case HasTemporalIntent(query):
		return "news"
	default:
		return "general"
	}
}

// dateLayouts 常见发布日期格式。
var dateLayouts = []string{
	"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02 15:04:05",
	"2006-01-02", "2006/01/02", "2006年01月02日", "2006.01.02",
	"Jan 2, 2006", "January 2, 2006", "02 Jan 2006",
}

// parsePublishDate 尽力解析发布日期，失败返回 zero time。
func parsePublishDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// RecencyFactor 新鲜度乘数因子。仅时间敏感查询生效；无有效日期返回 1.0。
//
//	< 7 天 ×1.5 | < 30 天 ×1.3 | < 90 天 ×1.1 | 其余 ×1.0
func RecencyFactor(temporal bool, publishDate string) float64 {
	if !temporal {
		return 1.0
	}
	t, ok := parsePublishDate(publishDate)
	if !ok {
		return 1.0
	}
	days := time.Since(t).Hours() / 24
	switch {
	case days < 0:
		return 1.0 // 未来日期，忽略
	case days < 7:
		return 1.5
	case days < 30:
		return 1.3
	case days < 90:
		return 1.1
	default:
		return 1.0
	}
}
