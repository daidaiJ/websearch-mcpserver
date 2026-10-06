package core

import (
	"sort"
	"fmt"
	"strings"
	"websearch/pkg/antirobot"
)

// ShowMeta 控制 MergeContent 输出中是否显示引擎来源和 score。
// 由工厂函数根据 smartsearch.show_meta 配置设置，默认 true。
var ShowMeta = true

// 日期来源注记三态（与 antirobot.Result.DateSource 同名透传）。
//   - structured：引擎 API / 结构化字段给出的日期（强，可直接采信）
//   - snippet：从页面展示文本 / 摘要解析出的日期（弱，仅用于定位来源）
//   - undated：无日期。结构中以空 PublishDate 表达，仅经 DateSourceState 规范化后出现
const (
	DateSourceStructured = antirobot.DateSourceStructured
	DateSourceSnippet    = antirobot.DateSourceSnippet
	DateSourceUndated    = "undated"
)

type SearchResult struct {
	Title       string  `json:"title"`
	Url         string  `json:"url"`
	Content     string  `json:"content"`
	PublishDate string  `json:"publishedDate"`
	DateSource  string  `json:"date_source,omitempty"` // PublishDate 来源注记（DateSource*，旧缓存行可能缺失）
	Score       float64 `json:"score,omitempty"`       // 搜索相关性分数（Tavily 等引擎回传，0 表示无分数）
	Engine      string  `json:"engine,omitempty"`      // 结果来源引擎名（首个返回该 URL 的引擎）
	Engines     []string `json:"engines,omitempty"`    // 返回该 URL 的全部引擎（Wigolo 评分增强的共识 Boost 使用）
	Type        string  `json:"type,omitempty"`        // "paper" 或 "web"，学术搜索时为 "paper"
	Authors     string  `json:"authors,omitempty"`     // 论文作者
	DOI         string  `json:"doi,omitempty"`         // 论文 DOI
	Journal     string  `json:"journal,omitempty"`     // 期刊/会议名
	CitedBy     int     `json:"cited_by,omitempty"`    // 被引次数
	PDFURL      string  `json:"pdf_url,omitempty"`     // PDF 链接
}

type SearchInf interface {
	Name() string
	Search(query string) (string, error)
	SearchRaw(query string) ([]SearchResult, error)
	MergeContent(query string, results []SearchResult) (string, error)
}

// SearchTimeRanger 支持按时间范围搜索的引擎可实现此可选接口。
// lookbackDays 控制搜索最近多少天的结果，0 表示使用引擎默认值。
type SearchTimeRanger interface {
	SearchRawWithTimeRange(query string, lookbackDays int) ([]SearchResult, error)
}

// AcademicSearchOptions 学术搜索可选参数。
type AcademicSearchOptions struct {
	Page      int      // 页码（默认 1）
	TimeRange string   // 时间范围: "year", "all"（默认 "all"）
	Engines   []string // 指定引擎子集（为空则使用全部已启用引擎）
}

// AcademicSearcher 支持学术搜索的引擎可实现此接口。
type AcademicSearcher interface {
	SearchAcademicRaw(query string, opts ...AcademicSearchOptions) (AcademicSearchResult, error)
	AcademicEngines() []string // 返回可用的学术引擎列表
}


// FormatScore 将 score 格式化为显示字符串，score <= 0 时返回空。
func FormatScore(score float64) string {
	if score <= 0 {
		return ""
	}
	return fmt.Sprintf("%.4f", score)
}

// AcademicSearchResult 学术搜索聚合结果：结果列表 + 逐引擎错误信息。
// EngineErrors 仅含本次失败的引擎（成功引擎不出现），不写入缓存。
type AcademicSearchResult struct {
	Results      []SearchResult
	EngineErrors map[string]string // engine name → error message
}

// ScoreBucket 单引擎按相关性排序的结果列表（提供 RRF 排名）。
type ScoreBucket struct {
	Name    string
	Weight  float64
	Results []SearchResult
}

// ParseTimeRange 将字符串转换为 antirobot.TimeRange。
func ParseTimeRange(s string) antirobot.TimeRange {
	switch s {
	case "day":
		return antirobot.TimeRangeDay
	case "week":
		return antirobot.TimeRangeWeek
	case "month":
		return antirobot.TimeRangeMonth
	case "year":
		return antirobot.TimeRangeYear
	default:
		return antirobot.TimeRangeNone
	}
}

// DateSourceState 规范化日期来源三态：无日期 → undated；有日期但无注记
// （旧缓存行 / 未注记引擎）→ 弱口径 snippet；有注记按注记返回。
func DateSourceState(r SearchResult) string {
	if strings.TrimSpace(r.PublishDate) == "" {
		return DateSourceUndated
	}
	if r.DateSource == DateSourceStructured {
		return DateSourceStructured
	}
	return DateSourceSnippet
}

// PreferDate 跨引擎合并同一结果时的日期采信规则：结构化来源优先于摘要解析
// （弱日期可被强日期覆盖），缺日期时补齐。date_source 注记随日期一同透传。
func PreferDate(dst *SearchResult, src SearchResult) {
	if src.PublishDate == "" {
		return
	}
	if src.DateSource == DateSourceStructured && dst.DateSource != DateSourceStructured {
		dst.PublishDate = src.PublishDate
		dst.DateSource = src.DateSource
		return
	}
	if dst.PublishDate == "" {
		dst.PublishDate = src.PublishDate
		dst.DateSource = src.DateSource
	}
}

// SortByScore 按 score 降序排序结果（score > 0 的优先）。
func SortByScore(results []SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

// FilterByScore 过滤掉 score < minScore 的结果。
// 引擎不回传 score（全部 score == 0）时不过滤。
func FilterByScore(results []SearchResult, minScore float64) []SearchResult {
	if minScore <= 0 {
		return results
	}
	hasScore := false
	for _, r := range results {
		if r.Score > 0 {
			hasScore = true
			break
		}
	}
	if !hasScore {
		return results
	}
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
