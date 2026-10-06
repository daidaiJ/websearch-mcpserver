package core

import (
	"fmt"
	"sort"
	"strings"
)

// FailureKind 引擎失败类型，与学术搜索逐引擎错误透传统一口径。
type FailureKind string

const (
	FailureTimeout   FailureKind = "timeout"    // 请求超时 / 上下文取消
	FailureRateLimit FailureKind = "rate_limit" // 429/202/限流冷却
	FailureChallenge FailureKind = "challenge"  // 验证码 / JS 挑战 / WAF
	FailureOffTopic  FailureKind = "off_topic"  // 整桶与查询不匹配（诱饵页），被整桶丢弃
	FailureError     FailureKind = "error"      // 无法归类的其它失败
)

// EngineFailure 单个引擎的失败记录。Reason 为短原因（截断后的错误摘要），
// 限流类失败带剩余冷却时间（"cooling down Xs"），即 gated/benched 透出口径。
type EngineFailure struct {
	Engine string      `json:"engine"`
	Kind   FailureKind `json:"kind"`
	Reason string      `json:"reason"`
}

// SearchDiagnostics 一次搜索的失败诊断。失败清单不受证据预算裁剪，永远随响应透出；
// 不写入缓存（与学术搜索 EngineErrors 同策略）。
type SearchDiagnostics struct {
	Failures    []EngineFailure `json:"failures"`               // 引擎失败 / 整桶丢弃清单
	FilterDrops map[string]int  `json:"filter_drops,omitempty"` // 过滤器名 → 丢弃条数（结果稀疏时透出）
}

// DiagnosticsProvider 编排层可实现的诊断查询接口。
// mcp 工具层通过类型断言读取最近一次搜索的诊断（无则不透出）。
type DiagnosticsProvider interface {
	LastDiagnostics() SearchDiagnostics
}

// AddFailures 追加引擎失败记录（engine 为空时忽略）。
func (d *SearchDiagnostics) AddFailures(engine string, kind FailureKind, reason string) {
	if engine == "" {
		return
	}
	d.Failures = append(d.Failures, EngineFailure{Engine: engine, Kind: kind, Reason: reason})
}

// AddFilterDrop 累计某过滤器丢弃条数。
func (d *SearchDiagnostics) AddFilterDrop(filter string, n int) {
	if n <= 0 {
		return
	}
	if d.FilterDrops == nil {
		d.FilterDrops = make(map[string]int)
	}
	d.FilterDrops[filter] += n
}

// HasFailures 是否存在需要透出的失败。
func (d *SearchDiagnostics) HasFailures() bool {
	return d != nil && len(d.Failures) > 0
}

// maxFailureReasonLen 短原因截断长度：保留归因信息，避免上游错误细节刷屏。
const maxFailureReasonLen = 160

// ShortReason 截断错误消息为短原因（单行）。
func ShortReason(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > maxFailureReasonLen {
		s = s[:maxFailureReasonLen] + "…"
	}
	return s
}

// ClassifyFailure 按错误消息关键词归类失败类型。
// 各引擎错误消息为中文描述或上游原始信息，按常见关键词匹配；无法归类时为 error。
func ClassifyFailure(err error) FailureKind {
	if err == nil {
		return FailureError
	}
	msg := strings.ToLower(err.Error())
	switch {
	case containsAny(msg, "timeout", "deadline", "超时", "context canceled", "context deadline"):
		return FailureTimeout
	case containsAny(msg, "429", "202", "rate-limit", "rate limit", "限流", "冷却", "cooling", "too many requests"):
		return FailureRateLimit
	case containsAny(msg, "验证码", "captcha", "挑战", "challenge", "wappass", "反爬", "waf"):
		return FailureChallenge
	case containsAny(msg, "off_topic", "off-topic"):
		return FailureOffTopic
	default:
		return FailureError
	}
}

func containsAny(msg string, keywords ...string) bool {
	for _, k := range keywords {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

// FormatFailures 渲染为 markdown 警告块，与学术搜索 MergeContentWithErrors 同口径。
// 无失败时返回空串。
func (d *SearchDiagnostics) FormatFailures() string {
	if !d.HasFailures() {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n> ⚠ 部分引擎本次失败，结果可能不完整：\n")
	failures := make([]EngineFailure, len(d.Failures))
	copy(failures, d.Failures)
	sort.SliceStable(failures, func(i, j int) bool { return failures[i].Engine < failures[j].Engine })
	for _, f := range failures {
		if f.Reason != "" {
			fmt.Fprintf(&b, "> - %s (%s): %s\n", f.Engine, f.Kind, f.Reason)
		} else {
			fmt.Fprintf(&b, "> - %s (%s)\n", f.Engine, f.Kind)
		}
	}
	return b.String()
}

// FormatFilterDrops 渲染稀疏结果的过滤诊断块：列出各过滤器丢弃数与放宽提示。
// 可解释性提示：过滤后结果 ≤3 条时由工具层调用，agent 据此决定是否放宽阈值重搜。
func (d *SearchDiagnostics) FormatFilterDrops() string {
	if d == nil || len(d.FilterDrops) == 0 {
		return ""
	}
	names := make([]string, 0, len(d.FilterDrops))
	total := 0
	for n, c := range d.FilterDrops {
		names = append(names, n)
		total += c
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s 丢弃 %d 条", n, d.FilterDrops[n]))
	}
	return fmt.Sprintf("\n> 🔍 过滤诊断（结果稀疏，共过滤掉 %d 条）： %s。可尝试放宽 smartsearch.relevance_threshold / engines.<name>.min_score 后重搜。\n", total, strings.Join(parts, "、"))
}

// FilterDropMinScore / FilterDropDedup 等为过滤器统计口径的固定命名，
// hybrid 编排层与 enhance 评分管线共用，供 filter_diagnostics 稳定透出。
const (
	FilterDropDedup     = "去重"
	FilterDropMinScore  = "min_score"
	FilterDropMaxSize   = "engine_max_size"
	FilterDropGlobalMax = "global_max_size"
	FilterDropEnhance   = "relevance_threshold"
)
