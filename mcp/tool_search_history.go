package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"websearch/pkg/history"
	"websearch/pkg/search/core"
)

// SearchHistoryParams search_history 工具参数。
type SearchHistoryParams struct {
	projectParams
	Query  string `json:"query,omitempty" jsonschema:"description,检索自己查过的搜索历史的关键词（FTS5 全文匹配查询与结果标题/摘要）；省略或空 = 返回最近历史（按时间倒序）"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"description,返回的历史条数上限（默认 10，硬上限 50）"`
	Offset *int   `json:"offset,omitempty" jsonschema:"description,翻页偏移量（默认 0）"`
}

// SearchHistoryHandler 检索本服务自己记录的搜索历史（FTS5 全文 + 最近列表），
// 让模型复用已查过的结果而不必重抓。只读本地库，不出网。
//
// 试验性（idea）：实用性尚未确认，默认关闭仅供体验；真机使用确认有价值后再转正式工具。
func SearchHistoryHandler(_ context.Context, _ *mcp.CallToolRequest, params *SearchHistoryParams) (*mcp.CallToolResult, any, error) {
	if historyInst == nil {
		return nil, nil, fmt.Errorf("search_history 未启用（search_history.enabled 默认关闭）")
	}
	limit, offset := 0, 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	records, err := historyInst.Search(params.Query, limit, offset)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: formatHistory(params.Query, records)}}}, nil, nil
}

// maxHistoryResultLines 单条历史最多展示的结果行数（超出提示已截断）。
const maxHistoryResultLines = 5

func formatHistory(query string, records []history.Record) string {
	var b strings.Builder
	if strings.TrimSpace(query) == "" {
		fmt.Fprintf(&b, "# 搜索历史（最近 %d 条）\n", len(records))
	} else {
		fmt.Fprintf(&b, "# 搜索历史匹配 %q（%d 条）\n", query, len(records))
	}
	if len(records) == 0 {
		b.WriteString("\n无历史记录。每次 smartsearch 成功后自动写入（search_history.enabled 开启时）。\n")
		return b.String()
	}
	for _, r := range records {
		fmt.Fprintf(&b, "\n## %s ｜ %s ｜ %d 条结果\n", r.Query, r.Mode, r.ResultCount)
		fmt.Fprintf(&b, "时间: %s\n", r.CreatedAt.Format("2006-01-02 15:04:05"))
		for i, res := range r.Results {
			if i >= maxHistoryResultLines {
				fmt.Fprintf(&b, "  - …另有 %d 条结果未展示\n", len(r.Results)-maxHistoryResultLines)
				break
			}
			b.WriteString(formatHistoryResult(i+1, res))
		}
	}
	b.WriteString("\n提示：以上为本地历史快照（正文截断）；需要完整正文用 cleanfetch 按 URL 重取。\n")
	return b.String()
}

func formatHistoryResult(idx int, r core.SearchResult) string {
	line := fmt.Sprintf("  %d. [%s](%s)", idx, strings.TrimSpace(r.Title), strings.TrimSpace(r.Url))
	if date := core.FormatDateSource(r.PublishDate, r.DateSource); date != "" {
		line += " ｜ " + date
	}
	if snippet := strings.TrimSpace(r.Content); snippet != "" {
		line += "\n     " + snippet
	}
	return line + "\n"
}
