package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/fetch/everything"
	"websearch/pkg/i18n"
	"websearch/pkg/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FileSearchParams file_search 工具参数。
type FileSearchParams struct {
	projectParams
	Query string `json:"query" jsonschema:"mcp.param.filesearch.query"`

	// 目录范围（白名单约束见 ScopeQuery）
	Folder string `json:"folder,omitempty" jsonschema:"mcp.param.filesearch.folder"`

	// Everything 原生过滤参数（透传 HTTP Server 的 i/w/r/m）
	MatchCase       bool `json:"match_case,omitempty" jsonschema:"mcp.param.filesearch.match_case"`
	WholeWord       bool `json:"whole_word,omitempty" jsonschema:"mcp.param.filesearch.whole_word"`
	MatchRegex      bool `json:"match_regex,omitempty" jsonschema:"mcp.param.filesearch.match_regex"`
	MatchDiacritics bool `json:"match_diacritics,omitempty" jsonschema:"mcp.param.filesearch.match_diacritics"`

	// 返回控制
	MaxResults int    `json:"max_results,omitempty" jsonschema:"mcp.param.filesearch.max_results"`
	Page       int    `json:"page,omitempty" jsonschema:"mcp.param.filesearch.page"`
	Sort       string `json:"sort,omitempty" jsonschema:"mcp.param.filesearch.sort"`
	Descending bool   `json:"descending,omitempty" jsonschema:"mcp.param.filesearch.descending"`

	// 二次过滤定制（覆盖服务端配置默认值）
	Exclude      []string `json:"exclude,omitempty" jsonschema:"mcp.param.filesearch.exclude"`
	MinAlignment *float64 `json:"min_alignment,omitempty" jsonschema:"mcp.param.filesearch.min_alignment"`

	TimeFormat string `json:"time_format,omitempty" jsonschema:"mcp.param.filesearch.time_format"`
}

// fileSearchSorts 允许的 sort 取值（Everything 原生排序）。
var fileSearchSorts = map[string]bool{"name": true, "path": true, "size": true, "date_modified": true}

// fileSearchCandidateHardCap 二次过滤的候选池硬上限（也决定最深可翻页数：
// 候选池触顶后更深的页码拿不到新候选）。
const fileSearchCandidateHardCap = 600

// fileSearchMaxPage 页码输入上限（防御性收敛；实际可翻深度由候选池上限决定）。
const fileSearchMaxPage = 100

// FileSearch 基于 Everything 索引的本地文件快速检索（只读，仅返回路径与元数据）。
// 服务端完成原生过滤（i/w/r/m），本地再做二次过滤：词汇对齐重排 + 噪声降权 +
// 可选阈值过滤，把最相关的结果排在前面并裁掉弱匹配，避免打爆上下文。
func FileSearch(ctx context.Context, req *mcp.CallToolRequest, params *FileSearchParams) (result *mcp.CallToolResult, extra any, err error) {
	requestID := telemetry.NewRequestID()
	ctx = telemetry.WithRequestID(ctx, requestID)
	started := time.Now()
	count := 0
	defer func() {
		telemetry.RecordEventContext(ctx, telemetry.Event{Kind: "tool", Tool: "file_search", Query: params.Query, Success: err == nil, Duration: time.Since(started), ResultCount: count, Error: err, RequestID: requestID, AttemptChain: recentAttemptChain(started, 15)})
	}()

	if everythingInst == nil {
		return nil, nil, errors.New(i18n.T("tool.filesearch.disabled"))
	}
	if strings.TrimSpace(params.Query) == "" {
		return nil, nil, errors.New(i18n.T("tool.filesearch.query_required"))
	}
	sort := strings.ToLower(strings.TrimSpace(params.Sort))
	if sort != "" && !fileSearchSorts[sort] {
		return nil, nil, errors.New(i18n.T("tool.filesearch.sort_unsupported", "Value", params.Sort))
	}
	timeFmt := fileSearchTimeFormats["datetime"]
	if params.TimeFormat != "" {
		f, ok := fileSearchTimeFormats[strings.ToLower(strings.TrimSpace(params.TimeFormat))]
		if !ok {
			return nil, nil, errors.New(i18n.T("tool.filesearch.time_format_unsupported", "Value", params.TimeFormat))
		}
		timeFmt = f
	}
	maxResults := config.ClampMaxResults(everythingMaxResults)
	maxClamped := false
	if params.MaxResults > 0 {
		maxResults = config.ClampMaxResults(params.MaxResults)
		maxClamped = params.MaxResults > maxResults
	}
	page, pageClamped := params.Page, false
	if page < 1 {
		page = 1
	}
	if page > fileSearchMaxPage {
		page, pageClamped = fileSearchMaxPage, true
	}

	// match_regex 用 Everything 的 regex: 函数拼入查询（而非 r=1 全局标志）：
	// r=1 会把包括目录限定项在内的整条查询都当正则，破坏 ScopeQuery 的范围限定
	userQuery := params.Query
	if params.MatchRegex {
		userQuery = `regex:"` + params.Query + `"`
	}
	userQuery = everything.AppendExcludes(userQuery, params.Exclude)
	query, err := everything.ScopeQuery(everythingRoots, params.Folder, userQuery)
	if err != nil {
		return nil, nil, err
	}

	// 调用级 min_alignment 覆盖服务端配置默认值；越界值收敛到 [0,1]
	minAlign := everythingMinAlign
	if params.MinAlignment != nil {
		minAlign = *params.MinAlignment
		if minAlign < 0 {
			minAlign = 0
		}
		if minAlign > 1 {
			minAlign = 1
		}
	}

	// 候选池按页码加深超采（单页上限的 3 倍，硬上限 600）：二次过滤会丢条，
	// 只有越过整页候选才能保证本页取到过滤结果集里的连续切片。
	candidates := fileSearchCandidates(page, maxResults)
	res, err := everythingInst.Search(ctx, query, everything.SearchOptions{
		Count:      candidates,
		Sort:       sort,
		Descending: params.Descending,
		MatchCase:  params.MatchCase,
		WholeWord:  params.WholeWord,
		Diacritics: params.MatchDiacritics,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", i18n.T("tool.filesearch.search_failed"), err)
	}

	// sort 显式指定时尊重服务端排序（只做阈值过滤）；默认 name 时才按对齐分重排
	items := everything.EnhanceItems(res.Items, params.Query, everything.EnhanceOptions{
		ReRank:    sort == "",
		NoiseDirs: everythingNoise,
		MinAlign:  minAlign,
	})
	pageItems, pageInfo := fileSearchPageSlice(items, page, maxResults, candidates, res.Total)
	pageInfo.MaxResultsClamped = maxClamped
	pageInfo.PageClamped = pageClamped
	pageInfo.Order = fileSearchOrderDesc(sort, params.Descending)
	count = len(pageItems)
	return textResult(formatFileSearchResult(query, pageInfo, pageItems, timeFmt)), nil, nil
}

// fileSearchCandidateOverFetch 候选超采倍数：给对齐重排与阈值过滤留出余量。
const fileSearchCandidateOverFetch = 3

// fileSearchCandidates 本次检索的候选池规模（随页码加深而增大，硬上限收敛）。
func fileSearchCandidates(page, pageSize int) int {
	n := page * pageSize * fileSearchCandidateOverFetch
	if n > fileSearchCandidateHardCap {
		n = fileSearchCandidateHardCap
	}
	return n
}

// fileSearchPageSlice 在二次过滤后的结果集上取第 page 页（1-based 连续切片，
// 页与页之间不重复也不跳条），并汇总渲染所需的分页信息。
// more 的判据：本页之后过滤结果集仍有条目，或服务端命中总数超过本次候选池
// 且候选池未触顶（触顶后加深页码拿不到新候选）。
func fileSearchPageSlice(items []everything.Item, page, pageSize, candidates int, total int64) ([]everything.Item, fileSearchPage) {
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, len(items))
	var pageItems []everything.Item
	if start < len(items) {
		pageItems = items[start:end]
	}
	info := fileSearchPage{
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
		Candidate: candidates,
		Capped:    candidates >= fileSearchCandidateHardCap,
	}
	info.More = len(items) > end || (int64(candidates) < total && !info.Capped)
	return pageItems, info
}

// fileSearchTimeFormats agent 可选的时间显示格式。
type fileSearchTimeFormat func(everything.Item) string

var fileSearchTimeFormats = map[string]fileSearchTimeFormat{
	// datetime：本地时区年月日时分秒，人类可读默认
	"datetime": func(it everything.Item) string {
		if t, ok := it.ModifiedTime(); ok {
			return t.Format("2006-01-02 15:04:05")
		}
		return ""
	},
	// iso：ISO 8601 UTC（服务端索引时间的机器可读形态）
	"iso": func(it everything.Item) string {
		if t, ok := it.ModifiedTime(); ok {
			return t.UTC().Format(time.RFC3339)
		}
		return ""
	},
	// filetime：Windows FILETIME 原始字符串（1601 起 100ns）
	"filetime": func(it everything.Item) string {
		return it.DateModified
	},
}

// fileSearchOrderDesc 渲染排序口径：未显式指定 sort 时本地按对齐分重排，
// 否则尊重服务端排序（含升降序），如实标注避免"按相关性"误标。
func fileSearchOrderDesc(sort string, descending bool) string {
	if sort == "" {
		return i18n.T("tool.filesearch.order_relevance")
	}
	if descending {
		return i18n.T("tool.filesearch.order_desc", "Sort", sort)
	}
	return i18n.T("tool.filesearch.order_asc", "Sort", sort)
}

// fileSearchPage 分页渲染上下文：本页元信息 + 命中总数与本次候选池规模。
type fileSearchPage struct {
	Page              int    // 当前页码（1-based）
	PageSize          int    // 每页条数上限（生效的 max_results）
	Total             int64  // Everything 命中总数
	Candidate         int    // 本次候选池条数（超采规模，硬上限 600）
	Order             string // 排序口径（未指定 sort 时为本地相关性重排）
	MaxResultsClamped bool   // agent 传的 max_results 超过硬上限被收敛
	PageClamped       bool   // agent 传的 page 超过上限被收敛
	Capped            bool   // 候选池已达硬上限
	More              bool   // 还有下一页
}

// formatFileSearchResult 将检索结果格式化为紧凑 Markdown（每条一行）。
// 每页只渲染本页条目；尾部提示按需给出翻页下一页、候选池触顶与弱匹配过滤三类去向，
// 参数被收敛时显式说明（不静默）。
func formatFileSearchResult(query string, pg fileSearchPage, items []everything.Item, fmtTime fileSearchTimeFormat) string {
	var sb strings.Builder
	if pg.Total == 0 {
		fmt.Fprintf(&sb, "%s", i18n.T("tool.filesearch.no_match", "Query", query))
		return sb.String()
	}
	if len(items) == 0 {
		if pg.Capped && pg.Total > int64(pg.Candidate) {
			fmt.Fprintf(&sb, "%s", i18n.T("tool.filesearch.page_capped_empty", "Total", pg.Total, "Page", pg.Page, "Cap", fileSearchCandidateHardCap))
		} else {
			fmt.Fprintf(&sb, "%s", i18n.T("tool.filesearch.page_last_empty", "Total", pg.Total, "Page", pg.Page))
		}
		return sb.String()
	}
	order := pg.Order
	if order == "" {
		order = i18n.T("tool.filesearch.order_relevance")
	}
	fmt.Fprintf(&sb, "%s\n\n", i18n.T("tool.filesearch.summary", "Total", pg.Total, "Page", pg.Page, "Count", len(items), "PageSize", pg.PageSize, "Order", order))
	for _, it := range items {
		// Everything 的 path 列不带尾分隔符，补上再拼文件名
		fmt.Fprintf(&sb, "- `%s\\%s`", it.Path, it.Name)
		if it.Type == "folder" {
			sb.WriteString(i18n.T("tool.filesearch.folder"))
		}
		if size := humanSize(it.Size); size != "" {
			fmt.Fprintf(&sb, " — %s", size)
		}
		if ts := fmtTime(it); ts != "" {
			fmt.Fprintf(&sb, " — %s", ts)
		}
		sb.WriteString("\n")
	}
	if pg.MaxResultsClamped {
		fmt.Fprintf(&sb, "\n%s", i18n.T("tool.filesearch.max_results_clamped", "Cap", config.EverythingMaxResultsHardCap, "PageSize", pg.PageSize))
	}
	if pg.PageClamped {
		fmt.Fprintf(&sb, "\n%s", i18n.T("tool.filesearch.page_clamped", "Max", fileSearchMaxPage, "Page", pg.Page))
	}
	switch {
	case pg.More:
		fmt.Fprintf(&sb, "\n%s", i18n.T("tool.filesearch.more_pages", "Next", pg.Page+1))
	case pg.Capped && pg.Total > int64(pg.Candidate):
		fmt.Fprintf(&sb, "\n%s", i18n.T("tool.filesearch.pool_capped", "Cap", fileSearchCandidateHardCap))
	case pg.Total > int64(len(items)):
		fmt.Fprintf(&sb, "\n%s", i18n.T("tool.filesearch.filtered", "Total", pg.Total, "Count", len(items)))
	}
	return sb.String()
}

// humanSize 字节数转可读大小；空或非法返回空串。
func humanSize(size string) string {
	if size == "" {
		return ""
	}
	var n int64
	if _, err := fmt.Sscanf(size, "%d", &n); err != nil || n <= 0 {
		return ""
	}
	const kb, mb, gb = 1024, 1024 * 1024, 1024 * 1024 * 1024
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
