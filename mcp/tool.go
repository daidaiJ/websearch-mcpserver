package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/jina"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/summarizer"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。
type SearchParamsWithIntent struct {
	Query    string `json:"query" jsonschema:"description,搜索关键词，例如 'Go并发编程' 或 '2024年新能源汽车销量'"`
	Intent   string `json:"intent" jsonschema:"description,搜索意图，描述你希望通过搜索解决什么问题或获取什么信息。例如 '了解goroutine调度原理' '对比React和Vue的生态差异' '查找某API的用法示例'。提供意图后可获得更精准的结构化摘要"`
	Academic bool   `json:"academic,omitempty" jsonschema:"description,是否启用学术搜索引擎进行学术论文检索。当需要查找论文、学术研究、文献综述、技术论文等学术相关内容时设为 true"`
}

// SearchParamsNoIntent LLM 摘要未启用时使用的参数（无 intent，节省上下文 token）。
type SearchParamsNoIntent struct {
	Query    string `json:"query" jsonschema:"description,搜索关键词，例如 'Go并发编程' 或 '2024年新能源汽车销量'"`
	Academic bool   `json:"academic,omitempty" jsonschema:"description,是否启用学术搜索引擎进行学术论文检索。当需要查找论文、学术研究、文献综述、技术论文等学术相关内容时设为 true"`
}

// CleanFetchParams cleanfetch 工具参数。
type CleanFetchParams struct {
	URL string `json:"url" jsonschema:"description,要抓取的网页 URL，例如 'https://example.com/article'"`
}

var (
	searchapi       search.SearchInf
	fallbackSearch  *search.BingSearchAdapter
	summarizerInst  *summarizer.Summarizer
	cacheInst       *cache.Cache
	jinaInst        *jina.Reader
	academicSearcher search.AcademicSearcher
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

func GetCache() *cache.Cache {
	return cacheInst
}

// isAcademicQuery 判断查询是否为学术意图。
func isAcademicQuery(query, intent string) bool {
	keywords := []string{
		"论文", "paper", "学术", "academic", "研究", "research",
		"文献", "journal", "arxiv", "doi", "引用", "citation",
		"发表", "publish", "期刊", "会议", "conference",
		"preprint", "预印本", "综述", "survey", "review",
		"算法", "algorithm", "模型", "model",
	}
	combined := strings.ToLower(query + " " + intent)
	for _, kw := range keywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return false
}

// ── WebSearch 处理函数（两个版本适配不同 Params） ─────────────────────────────

// WebSearchWithIntent LLM 启用时的 tool handler。
func WebSearchWithIntent(ctx context.Context, req *mcp.CallToolRequest, params *SearchParamsWithIntent) (*mcp.CallToolResult, any, error) {
	return doWebSearch(params.Query, params.Intent, params.Academic)
}

// WebSearchNoIntent LLM 未启用时的 tool handler。
func WebSearchNoIntent(ctx context.Context, req *mcp.CallToolRequest, params *SearchParamsNoIntent) (*mcp.CallToolResult, any, error) {
	return doWebSearch(params.Query, "", params.Academic)
}

// doWebSearch 搜索核心逻辑，两个 handler 共用。
func doWebSearch(query, intent string, academic bool) (*mcp.CallToolResult, any, error) {
	if searchapi == nil {
		return nil, nil, fmt.Errorf("api 初始化未完成")
	}

	// ---- 缓存查询 ----
	if cacheInst != nil {
		rec, hitType, err := cacheInst.Lookup(query, intent)
		if err != nil {
			log.Errf("缓存查询异常，跳过缓存: %v", err)
		} else if rec != nil {
			switch hitType {
			case "exact_intent":
				if rec.Summary != "" {
					log.Infof("缓存命中(exact_intent+summary): query=%s", query)
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: rec.Summary}}}, nil, nil
				}
				return nil, nil, fmt.Errorf("缓存命中(exact_intent)，但摘要为空")

			case "query_only":
				results, parseErr := rec.GetRawResults()
				if parseErr == nil {
					log.Infof("缓存命中(query_only): query=%s", query)
					ret, mergeErr := formatRawResults(query, results)
					if mergeErr != nil {
						return nil, nil, mergeErr
					}
					if intent != "" && summarizerInst != nil && rec.Summary == "" {
						go func() {
							defer func() {
								if r := recover(); r != nil {
									log.Errf("异步摘要 panic: %v", r)
								}
							}()
							output, sumErr := summarizerInst.Summarize(query, intent, results)
							if sumErr == nil {
								_ = cacheInst.UpdateSummary(query, intent, output)
								log.Infof("后台异步摘要完成: query=%s, intent=%s", query, intent)
							}
						}()
					}
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
				}
			}
		}
	}

	// ---- 确定是否使用学术搜索 ----
	useAcademic := academic || isAcademicQuery(query, intent)
	var results []search.SearchResult
	var err error

	if useAcademic && academicSearcher != nil {
		log.Infof("使用学术搜索引擎: query=%s, explicit=%v", query, academic)
		results, err = academicSearcher.SearchAcademicRaw(query)
		if err != nil {
			log.Errf("学术搜索失败，回退到通用搜索: %v", err)
			results = nil
		}
	}

	if results == nil {
		results, err = searchapi.SearchRaw(query)
		if err != nil {
			if fallbackSearch != nil && searchapi != fallbackSearch {
				log.Errf("主搜索引擎失败(%v)，回退到 Bing 引擎", err)
				results, err = fallbackSearch.SearchRaw(query)
			}
			if err != nil {
				return nil, nil, err
			}
		}
	}

	// 有 intent 且 LLM 可用 → 生成摘要
	if intent != "" && summarizerInst != nil {
		output, sumErr := summarizerInst.Summarize(query, intent, results)
		if sumErr == nil {
			if cacheInst != nil {
				_ = cacheInst.Store(query, intent, results, output)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
		}
		log.Errf("LLM 摘要失败，回退到原始结果: %v", sumErr)
	}

	ret, err := formatRawResults(query, results)
	if err != nil {
		return nil, nil, err
	}
	if cacheInst != nil {
		_ = cacheInst.Store(query, intent, results, "")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

func formatRawResults(query string, results []search.SearchResult) (string, error) {
	return searchapi.MergeContent(query, results)
}

// ── CleanFetch 工具 ──────────────────────────────────────────────────────────

// CleanFetch 通过 Jina Reader API 抓取网页并返回清理后的内容。
func CleanFetch(ctx context.Context, req *mcp.CallToolRequest, params *CleanFetchParams) (*mcp.CallToolResult, any, error) {
	if jinaInst == nil {
		return nil, nil, fmt.Errorf("jina reader 未初始化")
	}
	if params.URL == "" {
		return nil, nil, fmt.Errorf("url 参数不能为空")
	}

	result, err := jinaInst.Fetch(params.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("jina reader 抓取失败: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", result.Title)
	if result.Description != "" {
		fmt.Fprintf(&sb, "> %s\n\n", result.Description)
	}
	if result.PublishedTime != "" {
		fmt.Fprintf(&sb, "**发布时间**: %s\n\n", result.PublishedTime)
	}
	sb.WriteString(result.Content)

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}}}, nil, nil
}
