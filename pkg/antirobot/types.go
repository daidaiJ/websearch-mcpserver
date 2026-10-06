package antirobot

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── 网络区域 ──

// NetworkRegion 网络环境分类。
type NetworkRegion int

const (
	// RegionChina 国内网络友好，无需代理即可稳定访问。
	RegionChina NetworkRegion = iota
	// RegionInternational 海外服务，国内可能需要代理。
	RegionInternational
)

// ── 时间范围 ──

// TimeRange 时间范围过滤。
type TimeRange int

const (
	TimeRangeNone  TimeRange = iota // 不限
	TimeRangeDay                    // 最近 24 小时
	TimeRangeWeek                   // 最近一周
	TimeRangeMonth                  // 最近一月
	TimeRangeYear                   // 最近一年
)

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

// ── 结果类型 ──

// ResultType 结果类型。
type ResultType string

const (
	ResultWeb   ResultType = "web"   // 通用网页
	ResultPaper ResultType = "paper" // 学术论文
)

// ── 日期来源注记 ──

// 日期来源注记（date_source）。core.SearchResult 同名透传：
//   - structured：引擎 API / 结构化字段给出的日期（强，可直接采信）
//   - snippet：从页面展示文本 / 摘要解析出的日期（弱，仅用于定位来源）
//
// 无日期以空 PublishDate 表达，"undated" 三态由 core.DateSourceState 规范化得出。
const (
	DateSourceStructured = "structured"
	DateSourceSnippet    = "snippet"
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
	DateSource  string     `json:"date_source,omitempty"` // PublishDate 的来源注记（DateSourceStructured / DateSourceSnippet）
	DOI         string     `json:"doi,omitempty"`
	Journal     string     `json:"journal,omitempty"`
	CitedBy     int        `json:"cited_by,omitempty"`
	Score       float64    `json:"score,omitempty"`
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
		sb.WriteString(TruncateRunes(r.Content, 300))
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

// ── 去重与排序 ──

// DeduplicateResults 按去重键去重（DOI 优先，无 DOI 回退 URL），保留首次出现。
// 同一篇论文在不同引擎的落地页 URL 几乎必然不同，仅按 URL 去重会漏掉跨源重复。
func DeduplicateResults(results []Result) []Result {
	seen := make(map[string]struct{}, len(results))
	out := make([]Result, 0, len(results))
	for _, r := range results {
		k := resultKey(r)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, r)
	}
	return out
}

// resultKey 结果去重键：DOI 优先，无 DOI 回退 URL（保持原有精确匹配语义）。
func resultKey(r Result) string {
	if doi := NormalizeDOI(r.DOI); doi != "" {
		return "doi:" + doi
	}
	return "url:" + r.URL
}

// NormalizeDOI 归一化 DOI：小写、去 DOI 解析站前缀。
func NormalizeDOI(doi string) string {
	doi = strings.ToLower(strings.TrimSpace(doi))
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	return doi
}

// NormalizeAndSortResults 对去重后的结果按引擎分组归一化分数，再全局降序排序。
func NormalizeAndSortResults(results []Result) []Result {
	if len(results) <= 1 {
		return results
	}

	type group struct {
		results []Result
		maxAPI  float64
		hasAPI  bool
	}
	groups := make(map[string]*group)
	order := make([]string, 0)

	for _, r := range results {
		g, ok := groups[r.Engine]
		if !ok {
			g = &group{}
			groups[r.Engine] = g
			order = append(order, r.Engine)
		}
		g.results = append(g.results, r)
		if r.Score > 0 {
			g.hasAPI = true
			if r.Score > g.maxAPI {
				g.maxAPI = r.Score
			}
		}
	}

	for _, name := range order {
		g := groups[name]
		if g.hasAPI && g.maxAPI > 0 {
			for i := range g.results {
				g.results[i].Score = g.results[i].Score / g.maxAPI
			}
		} else {
			for i := range g.results {
				g.results[i].Score = 1.0 / float64(i+1)
			}
		}
	}

	merged := make([]Result, 0, len(results))
	for _, name := range order {
		merged = append(merged, groups[name].results...)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	return merged
}

// TruncateRunes 截断字符串到指定 rune 长度。
func TruncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
