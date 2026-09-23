package mcpserver

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/fetch/jina"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/llm"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"
)

// searchBaseParams smartsearch 两个工具定义（LLM 摘要开/关）共用的参数集合，
// 通过匿名嵌入组合：字段与 jsonschema 描述只在一处维护，SDK 会提升嵌入字段。
type searchBaseParams struct {
	Query     string `json:"query" jsonschema:"description,搜索关键词：需精准凝练地表达核心检索意图（建议 2-6 个关键词或一个短句），只保留最能定位目标的词；不要把同义词、过程词、修饰词一股脑堆砌成关键词列表，否则会稀释相关性。例如用 'Go 泛型 性能' 而非 'Go 泛型 类型参数 编译 运行时 性能 基准 对比 优化 使用方法'"`
	TimeRange int    `json:"time_range,omitempty" jsonschema:"description,搜索时间范围（月），限制搜索最近N个月的内容。例如 1=近1个月，3=近3个月，6=近半年，12=近一年。默认3，0表示不限"`
	FetchTopN *int   `json:"fetch_top_n,omitempty" jsonschema:"description,正文抓取模式：不传时与旧版一致，只返回引擎/供应商自带的内容（服务端 smartsearch.fetch_top_n 可改默认，默认 0 不抓）；传 0 表示只要标题摘要和 URL；传 1-5 表示为前 N 条获取页面原文——支持原文传参的 API 引擎直接走快速路径，其余引擎内部抓取页面。抓取被反爬防护（JS 挑战/WAF）拦截时会在该条结果上标注"`
}

type SearchParamsWithIntent struct {
	searchBaseParams
	Intent string `json:"intent" jsonschema:"description,搜索意图，描述你希望通过搜索解决什么问题或获取什么信息。例如 '了解goroutine调度原理' '对比React和Vue的生态差异' '查找某API的用法示例'。提供意图后可获得更精准的结构化摘要"`
}

// SearchParamsNoIntent LLM 摘要未启用时使用的参数（无 intent，节省上下文 token）。
type SearchParamsNoIntent struct {
	searchBaseParams
}

// AcademicSearchParams 学术搜索参数。
type AcademicSearchParams struct {
	Query     string   `json:"query" jsonschema:"description,学术搜索关键词，例如 'transformer attention mechanism' 或 'CRISPR gene editing'"`
	Engines   []string `json:"engines,omitempty" jsonschema:"description,指定引擎子集（为空则使用全部已启用引擎）。示例: 医学论文用 [\"pubmed\"], CS预印本用 [\"arxiv\"], 物理/数学用 [\"arxiv\",\"crossref\"]"`
	TimeRange string   `json:"time_range,omitempty" jsonschema:"description,时间范围过滤。可选值: year（近一年）, month（近一月）, week（近一周）, day（近一天）。为空则不限"`
	Page      int      `json:"page,omitempty" jsonschema:"description,结果页码（默认 1），每页约 10 条"`
}

// CleanFetchParams cleanfetch 工具参数。
type CleanFetchParams struct {
	URL  string   `json:"url" jsonschema:"description,要抓取的网页 URL，例如 'https://example.com/article'"`
	URLs []string `json:"urls,omitempty" jsonschema:"description,可选批量抓取：与 url 合并去重后最多 5 个。每条独立预检与抓取，单条失败不影响其它，结果按 URL 分节返回"`
}

// maxBatchFetchURLs 单次批量抓取的 URL 上限。
const maxBatchFetchURLs = 5

var (
	searchapi           search.SearchInf
	searchGroup         *search.SearchGroup
	fallbackSearch      *search.BingSearchAdapter
	summarizerInst      *llm.Summarizer
	cacheInst           *cache.Cache
	jinaInst            *jina.Reader
	webfetchInst        *webfetch.Fetcher
	academicSearcher    search.AcademicSearcher
	smartSearchConf     config.SmartSearchConfig
	cleanFetchMaxSizeMB int
	pdfMaxPages         int

	// webfetchLazyCfg 保存 Init 时的配置，供 fetch_top_n 在
	// cleanfetch/pdf_parser 均未启用时惰性初始化 webfetch（F1）。
	webfetchLazyCfg *config.Config
	webfetchMu      sync.Mutex
)

// Init 初始化 MCP 服务组件，通过 Option 模式按需加载。
func Init(conf config.Config, opts ...ServerOption) error {
	for _, opt := range opts {
		opt()
	}

	if searchapi == nil {
		return fmt.Errorf("搜索引擎未初始化，请检查配置")
	}
	return nil
}

// recentAttemptChain summarizes which providers were attempted during one
// tool call and who returned the winning result. It reads the just-recorded
// provider events and is therefore best-effort: a missing event only means
// the chain is shorter, never that the tool call failed.
func recentAttemptChain(started time.Time, limit int) string {
	store := telemetry.Default()
	if store == nil {
		return ""
	}
	events, err := store.RecentFiltered(telemetry.EventFilter{Kind: "provider", Limit: limit})
	if err != nil {
		return ""
	}
	seen := map[string]bool{}
	var attempted []string
	winner := ""
	for _, event := range events {
		if !withinCall(event.OccurredAt, started) {
			continue
		}
		if event.Provider == "" || seen[event.Provider] {
			continue
		}
		seen[event.Provider] = true
		attempted = append(attempted, event.Provider)
		if event.Success && winner == "" {
			winner = event.Provider
		}
	}
	if len(attempted) == 0 {
		return ""
	}
	if winner == "" {
		return strings.Join(attempted, "→")
	}
	return strings.Join(attempted, "→") + " · 结果来自 " + winner
}

func withinCall(occurredAt string, started time.Time) bool {
	if occurredAt == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, occurredAt)
	if err != nil {
		return true
	}
	return !parsed.Before(started.Add(-2 * time.Second))
}

func GetCache() *cache.Cache {
	return cacheInst
}

// GetSearchGroup 返回搜索引擎组（供 server 包与 SearXNG 共用同一套引擎）。
func GetSearchGroup() *search.SearchGroup {
	return searchGroup
}

// GetWebFetch 返回 WebFetch 引擎实例（供 server 包关闭时清理）。
func GetWebFetch() *webfetch.Fetcher {
	return webfetchInst
}
