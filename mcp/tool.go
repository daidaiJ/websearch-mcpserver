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
	"websearch/pkg/webfetch"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。
type SearchParamsWithIntent struct {
	Query  string `json:"query" jsonschema:"description,搜索关键词，例如 'Go并发编程' 或 '2024年新能源汽车销量'"`
	Intent string `json:"intent" jsonschema:"description,搜索意图，描述你希望通过搜索解决什么问题或获取什么信息。例如 '了解goroutine调度原理' '对比React和Vue的生态差异' '查找某API的用法示例'。提供意图后可获得更精准的结构化摘要"`
}

// SearchParamsNoIntent LLM 摘要未启用时使用的参数（无 intent，节省上下文 token）。
type SearchParamsNoIntent struct {
	Query string `json:"query" jsonschema:"description,搜索关键词，例如 'Go并发编程' 或 '2024年新能源汽车销量'"`
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
	URL string `json:"url" jsonschema:"description,要抓取的网页 URL，例如 'https://example.com/article'"`
}

var (
	searchapi       search.SearchInf
	fallbackSearch  *search.BingSearchAdapter
	summarizerInst  *summarizer.Summarizer
	cacheInst       *cache.Cache
	jinaInst        *jina.Reader
	webfetchInst    *webfetch.Fetcher
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

// GetWebFetch 返回 WebFetch 引擎实例（供 server 包关闭时清理）。
func GetWebFetch() *webfetch.Fetcher {
	return webfetchInst
}

// ── WebSearch 处理函数（两个版本适配不同 Params） ─────────────────────────────

// WebSearchWithIntent LLM 启用时的 tool handler。
func WebSearchWithIntent(ctx context.Context, req *mcp.CallToolRequest, params *SearchParamsWithIntent) (*mcp.CallToolResult, any, error) {
	return doWebSearch(params.Query, params.Intent)
}

// WebSearchNoIntent LLM 未启用时的 tool handler。
func WebSearchNoIntent(ctx context.Context, req *mcp.CallToolRequest, params *SearchParamsNoIntent) (*mcp.CallToolResult, any, error) {
	return doWebSearch(params.Query, "")
}

// AcademicSearchHandler 学术搜索 tool handler。
func AcademicSearchHandler(ctx context.Context, req *mcp.CallToolRequest, params *AcademicSearchParams) (*mcp.CallToolResult, any, error) {
	return doAcademicSearch(params.Query, params.Engines, params.TimeRange, params.Page)
}

// doWebSearch 通用网页搜索逻辑。
func doWebSearch(query, intent string) (*mcp.CallToolResult, any, error) {
	if searchapi == nil {
		return nil, nil, fmt.Errorf("api 初始化未完成")
	}

	// ---- 缓存查询 ----
	if cacheInst != nil {
		rec, hitType, err := cacheInst.Lookup(query, intent, false)
		if err != nil {
			log.Errf("缓存查询异常，跳过缓存: %v", err)
		} else if rec != nil && !rec.Academic {
			switch hitType {
			case "exact_intent":
				if rec.Summary != "" {
					log.Infof("缓存命中(exact_intent+summary): query=%s", query)
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: rec.Summary}}}, nil, nil
				}
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

	// ---- 搜索 ----
	results, err := searchapi.SearchRaw(query)
	if err != nil {
		if fallbackSearch != nil && searchapi != fallbackSearch {
			log.Errf("主搜索引擎失败(%v)，回退到 Bing 引擎", err)
			results, err = fallbackSearch.SearchRaw(query)
		}
		if err != nil {
			return nil, nil, err
		}
	}

	// 有 intent 且 LLM 可用 → 生成摘要
	if intent != "" && summarizerInst != nil {
		output, sumErr := summarizerInst.Summarize(query, intent, results)
		if sumErr == nil {
			if cacheInst != nil {
				_ = cacheInst.Store(query, intent, false, results, output)
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
		_ = cacheInst.Store(query, intent, false, results, "")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

// doAcademicSearch 学术搜索逻辑。
func doAcademicSearch(query string, engines []string, timeRange string, page int) (*mcp.CallToolResult, any, error) {
	if academicSearcher == nil {
		return nil, nil, fmt.Errorf("学术搜索引擎未启用，请检查配置 bing.academic 是否为 true")
	}

	// ---- 缓存查询 ----
	enginesKey := strings.Join(engines, ",")
	cacheKey := query + "|" + timeRange + "|" + enginesKey
	if cacheInst != nil {
		rec, hitType, err := cacheInst.Lookup(cacheKey, "", true)
		if err != nil {
			log.Errf("缓存查询异常，跳过缓存: %v", err)
		} else if rec != nil && rec.Academic && hitType == "query_only" {
			results, parseErr := rec.GetRawResults()
			if parseErr == nil {
				log.Infof("学术缓存命中: query=%s", query)
				ret, mergeErr := formatAcademicResults(query, results)
				if mergeErr == nil {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
				}
			}
		}
	}

	// ---- 学术搜索 ----
	opts := search.AcademicSearchOptions{
		Page:      page,
		TimeRange: timeRange,
		Engines:   engines,
	}

	log.Infof("学术搜索: query=%s, engines=%v, timeRange=%s, page=%d", query, engines, timeRange, page)
	results, err := academicSearcher.SearchAcademicRaw(query, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("学术搜索失败: %w", err)
	}

	ret, err := formatAcademicResults(query, results)
	if err != nil {
		return nil, nil, err
	}
	if cacheInst != nil {
		_ = cacheInst.Store(cacheKey, "", true, results, "")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

func formatAcademicResults(query string, results []search.SearchResult) (string, error) {
	if adapter, ok := academicSearcher.(*search.AcademicAdapter); ok {
		return adapter.MergeContent(query, results)
	}
	return searchapi.MergeContent(query, results)
}

func formatRawResults(query string, results []search.SearchResult) (string, error) {
	return searchapi.MergeContent(query, results)
}

// ── CleanFetch 工具 ──────────────────────────────────────────────────────────

// PDFParserParams pdf_parser 工具参数。
type PDFParserParams struct {
	Path string `json:"path" jsonschema:"description,本地 PDF 文件路径（支持绝对路径或 file:// 前缀）"`
}

// CleanFetch 通过 go-webfetch 抓取网页，失败时回退到 Jina Reader。
func CleanFetch(ctx context.Context, req *mcp.CallToolRequest, params *CleanFetchParams) (*mcp.CallToolResult, any, error) {
	if params.URL == "" {
		return nil, nil, fmt.Errorf("url 参数不能为空")
	}

	// ── 第一层：go-webfetch（无需代理）──
	if webfetchInst != nil {
		result, err := webfetchInst.Fetch(ctx, params.URL)
		if err == nil {
			return formatWebFetchResult(result), nil, nil
		}
		log.Infof("webfetch 抓取失败(%v)，尝试回退到 Jina Reader", err)

		// ── 第二层：Jina Reader（需代理，jinaInst != nil 即表示代理已开启）──
		if jinaInst != nil {
			jinaResult, jinaErr := jinaInst.Fetch(params.URL)
			if jinaErr == nil {
				return formatJinaResult(jinaResult), nil, nil
			}
			return nil, nil, fmt.Errorf("webfetch: %v; Jina 兜底: %w", err, jinaErr)
		}
		return nil, nil, fmt.Errorf("webfetch 抓取失败: %v", err)
	}

	// webfetch 未初始化，仅用 Jina（兼容旧模式：仅代理+Jina Key）
	if jinaInst != nil {
		jinaResult, jinaErr := jinaInst.Fetch(params.URL)
		if jinaErr != nil {
			return nil, nil, fmt.Errorf("jina reader 抓取失败: %w", jinaErr)
		}
		return formatJinaResult(jinaResult), nil, nil
	}

	return nil, nil, fmt.Errorf("webfetch 和 jina reader 均未初始化")
}

// PDFParserHandler 本地 PDF 解析 tool handler。
func PDFParserHandler(ctx context.Context, req *mcp.CallToolRequest, params *PDFParserParams) (*mcp.CallToolResult, any, error) {
	if params.Path == "" {
		return nil, nil, fmt.Errorf("path 参数不能为空")
	}
	if webfetchInst == nil {
		return nil, nil, fmt.Errorf("webfetch 未初始化，请先启用 cleanfetch")
	}

	// 确保路径带 file:// 前缀，Windows 路径需三斜杠
	pdfPath := params.Path
	if !strings.HasPrefix(pdfPath, "file://") {
		pdfPath = "file:///" + strings.ReplaceAll(pdfPath, `\`, "/")
	}

	result, err := webfetchInst.Fetch(ctx, pdfPath)
	if err != nil {
		return nil, nil, fmt.Errorf("PDF 解析失败: %v", err)
	}
	return formatWebFetchResult(result), nil, nil
}

// formatJinaResult 将 Jina Reader 结果格式化为 MCP 返回。
func formatJinaResult(result *jina.FetchResult) *mcp.CallToolResult {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", result.Title)
	if result.Description != "" {
		fmt.Fprintf(&sb, "> %s\n\n", result.Description)
	}
	if result.PublishedTime != "" {
		fmt.Fprintf(&sb, "**发布时间**: %s\n\n", result.PublishedTime)
	}
	sb.WriteString(result.Content)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}}}
}

// formatWebFetchResult 将 go-webfetch 结果格式化为 MCP 返回。
func formatWebFetchResult(result *webfetch.Result) *mcp.CallToolResult {
	var sb strings.Builder
	if result.Mode == "inline" {
		if result.Title != "" {
			fmt.Fprintf(&sb, "# %s\n\n", result.Title)
		}
		sb.WriteString(result.Markdown)
	} else {
		// 大文本已存储到文件
		if result.Title != "" {
			fmt.Fprintf(&sb, "# %s\n\n", result.Title)
		}
		fmt.Fprintf(&sb, "内容已保存到文件（共 %d 行，%d 字符）\n\n", result.TotalLines, result.TotalChars)
		fmt.Fprintf(&sb, "**文件路径**: `%s`\n\n", result.FilePath)
		if result.AgentHint != "" {
			fmt.Fprintf(&sb, "**读取提示**: %s\n", result.AgentHint)
		}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}}}
}
