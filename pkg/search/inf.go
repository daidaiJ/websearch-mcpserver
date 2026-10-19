package search

import (
	"websearch/pkg/antirobot"
	"websearch/pkg/search/adapter"
	"websearch/pkg/search/core"
	"websearch/pkg/search/hybrid"
)

// 类型契约层已下沉到子包 core（供 provider/adapter/enhance/hybrid 子包与编排层共用，避免循环引用）。
// 这里保留类型别名：外部包（mcp/cache/searxng）继续使用 search.XXX。
type (
	SearchResult            = core.SearchResult
	SearchInf               = core.SearchInf
	SearchTimeRanger        = core.SearchTimeRanger
	ContextSearcher         = core.ContextSearcher
	ContextTimeRanger       = core.ContextTimeRanger
	ContextAcademicSearcher = core.ContextAcademicSearcher
	AcademicSearchOptions   = core.AcademicSearchOptions
	AcademicSearcher        = core.AcademicSearcher
	AcademicSearchResult    = core.AcademicSearchResult

	// 编排层实现类型的别名（外部仅做类型断言/字段声明使用）。
	HybridSearchImpl  = hybrid.HybridSearchImpl
	BingSearchAdapter = adapter.BingSearchAdapter
	AcademicAdapter   = adapter.AcademicAdapter
)

// ParseTimeRange 将字符串转换为 antirobot.TimeRange。
func ParseTimeRange(s string) antirobot.TimeRange {
	return core.ParseTimeRange(s)
}
