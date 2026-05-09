package bing

import (
	"fmt"
	"strings"
	"time"
)

// Strategy 多引擎检索策略。
type Strategy int

const (
	// StrategyParallel 并行：同时请求所有引擎，汇总全部结果。
	StrategyParallel Strategy = iota
	// StrategySerial 串行：依次请求引擎，首个返回有效结果即停止。
	StrategySerial
)

// TimeRange 时间范围过滤。
type TimeRange int

const (
	TimeRangeNone  TimeRange = iota // 不限
	TimeRangeDay                    // 最近 24 小时
	TimeRangeWeek                   // 最近一周
	TimeRangeMonth                  // 最近一月
	TimeRangeYear                   // 最近一年
)

// NetworkRegion 网络环境分类。
type NetworkRegion int

const (
	// RegionChina 国内网络友好，无需代理即可稳定访问。
	RegionChina NetworkRegion = iota
	// RegionInternational 海外服务，国内可能需要代理。
	RegionInternational
)

// ResultType 结果类型。
type ResultType string

const (
	ResultWeb   ResultType = "web"   // 通用网页
	ResultPaper ResultType = "paper" // 学术论文
)

// Result 统一搜索结果。
type Result struct {
	Type        ResultType `json:"type"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Content     string     `json:"content"`
	PDFURL      string     `json:"pdf_url,omitempty"`
	Authors     string     `json:"authors,omitempty"`
	PublishedAt string     `json:"published_at,omitempty"`
	DOI         string     `json:"doi,omitempty"`
	Journal     string     `json:"journal,omitempty"`
	CitedBy     int        `json:"cited_by,omitempty"`
	Engine      string     `json:"engine"`
}

// SearchResponse 单引擎搜索响应。
type SearchResponse struct {
	Engine  string   `json:"engine"`
	Results []Result `json:"results"`
	Error   string   `json:"error,omitempty"`
}

// HasResults 响应是否包含有效结果。
func (r *SearchResponse) HasResults() bool {
	return r != nil && r.Error == "" && len(r.Results) > 0
}

// Engine 搜索引擎接口。
type Engine interface {
	Name() string
	Region() NetworkRegion
	Search(query string, page int, timeRange TimeRange) (*SearchResponse, error)
}

// ──────────────────────────────────────────────────────────────────────────────
// Options
// ──────────────────────────────────────────────────────────────────────────────

// Options 搜索模块总配置。
type Options struct {
	Strategy         Strategy
	Network          NetworkRegion
	Academic         bool
	MaxResults       int
	TimeRange        TimeRange
	Concurrency      int
	PerEngineTimeout time.Duration

	Bing            BingOpts
	Arxiv           ArxivOpts
	Crossref        CrossrefOpts
	OpenAlex        OpenAlexOpts
	SemanticScholar SemanticScholarOpts
}

// DefaultOptions 默认配置：串行策略，仅 Bing。
func DefaultOptions() Options {
	return Options{
		Strategy:         StrategySerial,
		Network:          RegionChina,
		Academic:         false,
		Concurrency:      5,
		PerEngineTimeout: 10 * time.Second,
		Bing:             BingOpts{Enabled: true, PerSec: 1, PerMin: 20},
	}
}

// AcademicOptions 启用学术引擎（全部国内可达源）。
func AcademicOptions() Options {
	o := DefaultOptions()
	o.Academic = true
	o.Arxiv.Enabled = true
	o.Crossref.Enabled = true
	o.OpenAlex.Enabled = true
	return o
}

// AcademicAllOptions 启用全部学术引擎（含海外源 Semantic Scholar）。
func AcademicAllOptions() Options {
	o := AcademicOptions()
	o.Network = RegionInternational
	o.SemanticScholar.Enabled = true
	return o
}

// ──────────────────────────────────────────────────────────────────────────────
// 各引擎 Options
// ──────────────────────────────────────────────────────────────────────────────

// BingOpts Bing 通用搜索配置。
type BingOpts struct {
	Enabled    bool
	Blocked    []string // 屏蔽域名
	PerSec     int      // 每秒限流，默认 1
	PerMin     int      // 每分钟限流，默认 20
	SafeSearch int      // 0=关, 1=中, 2=严
}

// ArxivOpts arXiv 预印本配置。
type ArxivOpts struct {
	Enabled bool
}

// CrossrefOpts Crossref 学术元数据配置。
type CrossrefOpts struct {
	Enabled bool
}

// OpenAlexOpts OpenAlex 开放学术图谱配置。
type OpenAlexOpts struct {
	Enabled bool
	MailTo  string // polite pool 邮箱（可选）
}

// SemanticScholarOpts Semantic Scholar 配置。
type SemanticScholarOpts struct {
	Enabled bool
}

// ──────────────────────────────────────────────────────────────────────────────
// Markdown 格式化（用于去重后统一输出）
// ──────────────────────────────────────────────────────────────────────────────

// Markdown 将单条结果格式化为 Markdown 片段。
func (r Result) Markdown() string {
	var sb strings.Builder

	if r.Type == ResultPaper {
		sb.WriteString(fmt.Sprintf("[%s](%s)\n", r.Title, r.URL))
		meta := []string{}
		if r.Authors != "" {
			meta = append(meta, "**"+r.Authors+"**")
		}
		if r.PublishedAt != "" {
			meta = append(meta, r.PublishedAt)
		}
		if r.Journal != "" {
			meta = append(meta, "_"+r.Journal+"_")
		}
		if r.DOI != "" {
			meta = append(meta, "DOI:`"+r.DOI+"`")
		}
		if r.CitedBy > 0 {
			meta = append(meta, fmt.Sprintf("%d citations", r.CitedBy))
		}
		if len(meta) > 0 {
			sb.WriteString(strings.Join(meta, " | "))
			sb.WriteString("\n")
		}
		if r.PDFURL != "" {
			sb.WriteString(fmt.Sprintf("[PDF](%s)", r.PDFURL))
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("[%s](%s)\n", r.Title, r.URL))
		if r.PublishedAt != "" {
			sb.WriteString(fmt.Sprintf("_%s_", r.PublishedAt))
			sb.WriteString("\n")
		}
	}

	if r.Content != "" {
		sb.WriteString(truncateRunes(r.Content, 300))
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatMarkdown 将去重后的结果列表按类型分组输出为 Markdown 文档。
func FormatMarkdown(results []Result) string {
	var papers, webs []Result
	for _, r := range results {
		if r.Type == ResultPaper {
			papers = append(papers, r)
		} else {
			webs = append(webs, r)
		}
	}

	var sb strings.Builder

	if len(papers) > 0 {
		sb.WriteString("## Papers\n\n")
		for i, r := range papers {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Markdown()))
		}
	}

	if len(webs) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString("## Web\n\n")
		for i, r := range webs {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Markdown()))
		}
	}

	return sb.String()
}

// DeduplicateResults 按 URL 去重，保留首次出现。
func DeduplicateResults(results []Result) []Result {
	seen := make(map[string]struct{}, len(results))
	out := make([]Result, 0, len(results))
	for _, r := range results {
		if _, dup := seen[r.URL]; dup {
			continue
		}
		seen[r.URL] = struct{}{}
		out = append(out, r)
	}
	return out
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// TimeRangeSince 返回 timeRange 对应的起始日期字符串（YYYY-MM-DD）。
func TimeRangeSince(tr TimeRange) string {
	now := time.Now()
	switch tr {
	case TimeRangeDay:
		return now.AddDate(0, 0, -1).Format("2006-01-02")
	case TimeRangeWeek:
		return now.AddDate(0, 0, -7).Format("2006-01-02")
	case TimeRangeMonth:
		return now.AddDate(0, -1, 0).Format("2006-01-02")
	case TimeRangeYear:
		return now.AddDate(-1, 0, 0).Format("2006-01-02")
	default:
		return ""
	}
}
