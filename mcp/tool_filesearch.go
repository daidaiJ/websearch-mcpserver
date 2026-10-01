package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"
	"websearch/pkg/fetch/everything"
	"websearch/pkg/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FileSearchParams file_search 工具参数。
type FileSearchParams struct {
	Query string `json:"query" jsonschema:"description,检索词，支持 Everything 搜索语法：如 'factory'、'*.go'、'ext:pdf report'、'dm:lastweek'、'size:>1mb'。不要写 '**/' 前缀，目录范围已由 folder 参数限定；正则请用 match_regex 参数"`

	// 目录范围（白名单约束见 ScopeQuery）
	Folder string `json:"folder,omitempty" jsonschema:"description,限定检索目录，接受 Windows（D:\\CODE\\ai）或 Git Bash（/d/code/ai）风格路径。配置了目录白名单时必须落在白名单内；省略时在白名单全部目录内检索（未配置白名单则为全盘索引）"`

	// Everything 原生过滤参数（透传 HTTP Server 的 i/w/r/m）
	MatchCase       bool `json:"match_case,omitempty" jsonschema:"description,区分大小写匹配"`
	WholeWord       bool `json:"whole_word,omitempty" jsonschema:"description,全字匹配"`
	MatchRegex      bool `json:"match_regex,omitempty" jsonschema:"description,把 query 当正则表达式检索（以 regex: 函数拼入，不影响目录限定；如 '^factory'）"`
	MatchDiacritics bool `json:"match_diacritics,omitempty" jsonschema:"description,区分变音符号"`

	// 返回控制
	MaxResults int    `json:"max_results,omitempty" jsonschema:"description,返回条数上限（默认 50，上限 200）。建议按需调小以节省上下文"`
	Sort       string `json:"sort,omitempty" jsonschema:"description,排序字段：name（默认）/ date_modified / size / path。找最近改动用 date_modified 配合 descending"`
	Descending bool   `json:"descending,omitempty" jsonschema:"description,是否降序（配合 sort 使用）"`

	// 二次过滤定制（覆盖服务端配置默认值）
	Exclude      []string `json:"exclude,omitempty" jsonschema:"description,排除项（字符串数组，每项一条 Everything NOT 词）：如 [\"\\\\obj\\\\\", \"\\\\node_modules\\\\\"]、\"old backup\"。按字面匹配路径片段或文件名，带首尾反斜杠才不会误伤名字里含该词的文件"`
	MinAlignment *float64 `json:"min_alignment,omitempty" jsonschema:"description,词汇对齐阈值（0~1）：查询词与文件名+路径对齐率低于该值的结果被丢弃。覆盖服务端 everything.min_alignment；0=只重排不过滤。结果过多时建议 0.3 起步"`

	TimeFormat string `json:"time_format,omitempty" jsonschema:"description,时间显示格式：datetime（默认，2026-01-02 15:04:05）/ iso（ISO 8601 UTC）/ filetime（Windows FILETIME 原始值）"`
}

// fileSearchSorts 允许的 sort 取值（Everything 原生排序）。
var fileSearchSorts = map[string]bool{"name": true, "path": true, "size": true, "date_modified": true}

// fileSearchCandidateHardCap 二次过滤的候选池硬上限。
const fileSearchCandidateHardCap = 600

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
		return nil, nil, fmt.Errorf("file_search 未启用（Everything HTTP Server 不可用）")
	}
	if strings.TrimSpace(params.Query) == "" {
		return nil, nil, fmt.Errorf("query 参数不能为空")
	}
	sort := strings.ToLower(strings.TrimSpace(params.Sort))
	if sort != "" && !fileSearchSorts[sort] {
		return nil, nil, fmt.Errorf("sort 仅支持 name / path / size / date_modified，收到 %q", params.Sort)
	}
	timeFmt := fileSearchTimeFormats["datetime"]
	if params.TimeFormat != "" {
		f, ok := fileSearchTimeFormats[strings.ToLower(strings.TrimSpace(params.TimeFormat))]
		if !ok {
			return nil, nil, fmt.Errorf("time_format 仅支持 datetime / iso / filetime，收到 %q", params.TimeFormat)
		}
		timeFmt = f
	}
	maxResults := everythingMaxResults
	if params.MaxResults > 0 {
		maxResults = params.MaxResults
		if maxResults > 200 {
			maxResults = 200
		}
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

	// 候选超采（3x，硬上限 600）给二次过滤留出排序空间，最终只返回 maxResults 条
	candidates := maxResults * 3
	if candidates > fileSearchCandidateHardCap {
		candidates = fileSearchCandidateHardCap
	}
	res, err := everythingInst.Search(ctx, query, everything.SearchOptions{
		Count:      candidates,
		Sort:       sort,
		Descending: params.Descending,
		MatchCase:  params.MatchCase,
		WholeWord:  params.WholeWord,
		Diacritics: params.MatchDiacritics,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Everything 检索失败: %w", err)
	}

	// sort 显式指定时尊重服务端排序（只做阈值过滤）；默认 name 时才按对齐分重排
	items := everything.EnhanceItems(res.Items, params.Query, everything.EnhanceOptions{
		ReRank:    sort == "",
		NoiseDirs: everythingNoise,
		MinAlign:  minAlign,
	})
	if len(items) > maxResults {
		items = items[:maxResults]
	}
	count = len(items)
	return textResult(formatFileSearchResult(query, int(res.Total), len(res.Items), items, timeFmt)), nil, nil
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

// formatFileSearchResult 将检索结果格式化为紧凑 Markdown（每条一行）。
func formatFileSearchResult(query string, total, candidate int, items []everything.Item, fmtTime fileSearchTimeFormat) string {
	var sb strings.Builder
	if total == 0 {
		fmt.Fprintf(&sb, "没有匹配 `%s` 的文件。可放宽关键词、检查目录范围或改用 ext:/dm: 等语法。", query)
		return sb.String()
	}
	fmt.Fprintf(&sb, "命中 %d 条，返回前 %d 条（按相关性排序）：\n\n", total, len(items))
	for _, it := range items {
		// Everything 的 path 列不带尾分隔符，补上再拼文件名
		fmt.Fprintf(&sb, "- `%s\\%s`", it.Path, it.Name)
		if it.Type == "folder" {
			sb.WriteString(" （目录）")
		}
		if size := humanSize(it.Size); size != "" {
			fmt.Fprintf(&sb, " — %s", size)
		}
		if ts := fmtTime(it); ts != "" {
			fmt.Fprintf(&sb, " — %s", ts)
		}
		sb.WriteString("\n")
	}
	if total > candidate {
		fmt.Fprintf(&sb, "\n命中过多，仅展示前 %d 条。请收窄 query（加 ext:/dm:/size: 或子目录）再查。", len(items))
	} else if total > len(items) {
		fmt.Fprintf(&sb, "\n低相关结果已过滤（候选 %d 条）。请收窄 query 或提高 max_results。", candidate)
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
