package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"websearch/pkg/history"
	"websearch/pkg/i18n"
	"websearch/pkg/search/core"
)

// SearchHistoryParams search_history 工具参数。
type SearchHistoryParams struct {
	projectParams
	Query  string `json:"query,omitempty" jsonschema:"mcp.param.history.query"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"mcp.param.history.limit"`
	Offset *int   `json:"offset,omitempty" jsonschema:"mcp.param.history.offset"`
}

// SearchHistoryHandler 检索本服务自己记录的搜索历史（FTS5 全文 + 最近列表），
// 让模型复用已查过的结果而不必重抓。只读本地库，不出网。
//
// 试验性（idea）：实用性尚未确认，默认关闭仅供体验；真机使用确认有价值后再转正式工具。
func SearchHistoryHandler(_ context.Context, _ *mcp.CallToolRequest, params *SearchHistoryParams) (*mcp.CallToolResult, any, error) {
	if historyInst == nil {
		return nil, nil, errors.New(i18n.T("tool.history.disabled"))
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

// maxHistoryQueryRunes 历史条目头部展示的 query 截断上限（原文仍完整存库，
// 检索用 FTS 不受影响；超长 query 多为误粘贴，全量展示只会撑爆响应）。
const maxHistoryQueryRunes = 200

// truncateHistoryQuery 历史展示用 query 截断。
func truncateHistoryQuery(q string) string {
	if runes := []rune(q); len(runes) > maxHistoryQueryRunes {
		return string(runes[:maxHistoryQueryRunes]) + "…"
	}
	return q
}

func formatHistory(query string, records []history.Record) string {
	var b strings.Builder
	if strings.TrimSpace(query) == "" {
		b.WriteString(i18n.T("tool.history.header_recent", "Count", len(records)))
		b.WriteString("\n")
	} else {
		b.WriteString(i18n.T("tool.history.header_match", "Query", truncateHistoryQuery(query), "Count", len(records)))
		b.WriteString("\n")
	}
	if len(records) == 0 {
		b.WriteString(i18n.T("tool.history.empty"))
		return b.String()
	}
	for _, r := range records {
		b.WriteString("\n")
		b.WriteString(i18n.T("tool.history.entry_header", "Query", truncateHistoryQuery(r.Query), "Mode", r.Mode, "Count", r.ResultCount))
		b.WriteString("\n")
		b.WriteString(i18n.T("tool.history.entry_time", "Time", r.CreatedAt.Format("2006-01-02 15:04:05")))
		b.WriteString("\n")
		for i, res := range r.Results {
			if i >= maxHistoryResultLines {
				b.WriteString(i18n.T("tool.history.more_results", "Count", len(r.Results)-maxHistoryResultLines))
				b.WriteString("\n")
				break
			}
			b.WriteString(formatHistoryResult(i+1, res))
		}
	}
	b.WriteString(i18n.T("tool.history.hint"))
	b.WriteString("\n")
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
