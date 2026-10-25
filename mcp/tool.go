package mcpserver

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/fetch/everything"
	"websearch/pkg/fetch/jina"
	"websearch/pkg/fetch/sitefetch"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/history"
	"websearch/pkg/i18n"
	"websearch/pkg/llm"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"
)

type projectParams struct {
	Project string `json:"project,omitempty" jsonschema:"mcp.param.project"`
}

// searchBaseParams smartsearch 两个工具定义（LLM 摘要开/关）共用的参数集合，
// 通过匿名嵌入组合：字段与 jsonschema 描述只在一处维护，SDK 会提升嵌入字段。
type searchBaseParams struct {
	projectParams
	Query     string `json:"query" jsonschema:"mcp.param.search.query"`
	TimeRange *int   `json:"time_range,omitempty" jsonschema:"mcp.param.search.time_range"`
	FetchTopN *int   `json:"fetch_top_n,omitempty" jsonschema:"mcp.param.search.fetch_top_n"`
}

type SearchParamsWithIntent struct {
	searchBaseParams
	Intent string `json:"intent" jsonschema:"mcp.param.search.intent"`
}

// SearchParamsNoIntent LLM 摘要未启用时使用的参数（无 intent，节省上下文 token）。
type SearchParamsNoIntent struct {
	searchBaseParams
}

// AcademicSearchParams 学术搜索参数。
type AcademicSearchParams struct {
	projectParams
	Query     string   `json:"query" jsonschema:"mcp.param.academic.query"`
	Engines   []string `json:"engines,omitempty" jsonschema:"mcp.param.academic.engines"`
	TimeRange string   `json:"time_range,omitempty" jsonschema:"mcp.param.academic.time_range"`
	Page      int      `json:"page,omitempty" jsonschema:"mcp.param.academic.page"`
}

// CleanFetchParams cleanfetch 工具参数。
type CleanFetchParams struct {
	projectParams
	URL       string   `json:"url,omitempty" jsonschema:"mcp.param.cleanfetch.url"`
	URLs      []string `json:"urls,omitempty" jsonschema:"mcp.param.cleanfetch.urls"`
	ResultRef string   `json:"result_ref,omitempty" jsonschema:"mcp.param.cleanfetch.result_ref"`
	Offset    *int64   `json:"offset,omitempty" jsonschema:"mcp.param.cleanfetch.offset"`
	Limit     *int     `json:"limit,omitempty" jsonschema:"mcp.param.cleanfetch.limit"`
}

// maxBatchFetchURLs 单次批量抓取的 URL 上限。
const maxBatchFetchURLs = 5

// mcpHubFetcher 托管 MCP 抓取能力（mcphub.Hub 实现），便于 cleanfetch 单测注入。
type mcpHubFetcher interface {
	Fetch(ctx context.Context, rawURL string) (string, error)
}

var (
	searchapi       search.SearchInf
	searchGroup     *search.SearchGroup
	fallbackSearch  *search.BingSearchAdapter
	summarizerInst  *llm.Summarizer
	cacheInst       *cache.Cache
	jinaInst        *jina.Reader
	webfetchInst    *webfetch.Fetcher
	mcpHubFetchInst mcpHubFetcher
	mcpHubFetchMode string
	// siteFetchGitHub cleanfetch 站点特判：github.com 仓库/blob 页改走 REST API
	// （HTML 是 JS 渲染抓不到），与 GitHub 引擎共用 conf.GitHub 的 token 提额。
	siteFetchGitHub *sitefetch.GitHubFetcher
	// providerFetchers 供应商自带 fetch API 的远端抓取链（TinyFish/Keenable 等），
	// 排在 mcphub 托管 MCP 之前：优先消耗供应商按 URL 计数的免费 fetch 额度。
	providerFetchers     []mcpHubFetcher
	historyInst          *history.Store
	academicSearcher     search.AcademicSearcher
	everythingInst       *everything.Client
	everythingRoots      []string
	everythingMaxResults int
	everythingNoise      map[string]struct{}
	everythingMinAlign   float64
	smartSearchConf      config.SmartSearchConfig
	cleanFetchMaxSizeMB  int
	pdfMaxPages          int
	// MinerU 本地裁切路径的分批与预算参数（0 值语义见 config.PDFParserConfig）。
	pdfMineruPageLimit  int
	pdfMineruPageBatch  int
	pdfMineruPageBudget int

	// webfetchLazyCfg 保存 Init 时的配置，供 fetch_top_n 在
	// cleanfetch/pdf_parser 均未启用时惰性初始化 webfetch（F1）。
	webfetchLazyCfg *config.Config
	webfetchMu      sync.Mutex

	// everythingLazyCfg 保存 Init 时的配置，供首个 MCP 客户端接入时
	// 惰性探测 Everything HTTP Server——websearch 与 Everything 都是
	// 自启动，启动期探测会因两者时序竞争导致 file_search 误判缺失。
	everythingLazyCfg *config.EverythingConfig
	everythingMu      sync.Mutex
)

// Init 初始化 MCP 服务组件，通过 Option 模式按需加载。
func Init(conf config.Config, opts ...ServerOption) error {
	publishTools(pendingTools())
	for _, opt := range opts {
		opt()
	}

	if searchapi == nil {
		return errors.New(i18n.T("tool.init.search_not_initialized"))
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
	return i18n.T("tool.chain.result_from", "Chain", strings.Join(attempted, "→"), "Winner", winner)
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

// GetSearchHistory 返回搜索历史存储实例（供 server 包关闭时清理；未启用为 nil）。
func GetSearchHistory() *history.Store {
	return historyInst
}
