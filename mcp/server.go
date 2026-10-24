package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"websearch/pkg/config"
	"websearch/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewMCPServer 按配置注册 MCP 工具，返回可挂到任意 transport 的 *mcp.Server。
func NewMCPServer(conf config.Config, opts *mcp.ServerOptions) *mcp.Server {
	if opts == nil {
		opts = &mcp.ServerOptions{}
	}
	if opts.KeepAlive == 0 {
		opts.KeepAlive = 30 * time.Second
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "websearch server",
		Version: serverVersion, // 与 search://capabilities 的版本同源（入口 main 注入，P1-10 回查对齐）
	}, opts)

	server.AddReceivingMiddleware(createLoggingMiddleware())
	server.AddReceivingMiddleware(ClientAttributionMiddleware)
	registerTools(server, conf)
	// 只读观测 Resource（capabilities/health）：mcp_resources: false 时整体关闭
	if conf.ResourcesEnabled() {
		registerResources(server, conf)
	}
	return server
}

func registerTools(server *mcp.Server, conf config.Config) {
	states := pendingTools()
	for i := range states {
		states[i].State = "disabled"
	}
	setState := func(name, state string) {
		for i := range states {
			if states[i].Name == name {
				states[i].State = state
				states[i].Registered = state == "registered"
			}
		}
	}
	// ── 注册 smartsearch 工具 ──
	if smartsearchAvailable() {
		searchDesc := "通用联网检索工具，获取实时信息（新闻/技术文档/产品/数据等）。查询词需精准凝练、聚焦核心意图，避免堆砌大量同义/次要词形成关键词列表（会稀释相关性）。"
		if conf.LLMEnabled() {
			searchDesc += "可用 intent 参数说明检索目的以获得更精准的结构化摘要。"
		}
		searchDesc += "可用 fetch_top_n 对评分最高的前 N 条抓取正文（默认 0 不抓，上限 5）。"
		if fallbackSearch != nil {
			searchDesc += "主引擎不可用时自动回退 Bing。"
		}
		searchDesc += "适用边界：时效性事实、网页线索与文档检索；系统性文献综述、需要引用数据的学术问题请改用 academicsearch。"

		if conf.LLMEnabled() {
			mcp.AddTool(server, &mcp.Tool{
				Name:        "smartsearch",
				Description: searchDesc,
			}, WebSearchWithIntent)
			log.Info("Available tool: smartsearch (with intent)")
		} else {
			mcp.AddTool(server, &mcp.Tool{
				Name:        "smartsearch",
				Description: searchDesc,
			}, WebSearchNoIntent)
			log.Info("Available tool: smartsearch (no intent, LLM disabled)")
		}
		setState("smartsearch", "registered")
	} else {
		setState("smartsearch", "unavailable")
	}

	// ── 注册 academicsearch 工具 ──
	if conf.Academic.Enabled && academicSearcher != nil {
		acadDesc := buildAcademicToolDescription()
		mcp.AddTool(server, &mcp.Tool{
			Name:        "academicsearch",
			Description: acadDesc,
		}, AcademicSearchHandler)
		log.Infof("Available tool: academicsearch (engines: %v)", academicSearcher.AcademicEngines())
		setState("academicsearch", "registered")
	} else if conf.Academic.Enabled {
		setState("academicsearch", "unavailable")
	}

	// Saved-body reads remain available even when URL fetching is disabled.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cleanfetch",
		Description: "正文获取工具。url/urls 抓取网页 Markdown（合并去重最多 5 个）；或传原工具返回的 result_ref 分段读取已保存的搜索、网页或 PDF 正文，返回 content、next_offset、eof，不重新请求来源。两种模式互斥；offset/limit 仅用于引用读取。始终注册，cleanfetch.enabled 仅控制 URL 抓取。发现新来源请先用 smartsearch。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *CleanFetchParams) (*mcp.CallToolResult, any, error) {
		return cleanFetch(ctx, req, params, conf.CleanFetch.Enabled)
	})
	log.Info("Available tool: cleanfetch")
	setState("cleanfetch", "registered")

	// ── 注册 file_search 工具（接入时探测通过才暴露，见 ensureEverything） ──
	if ensureEverything() {
		fileDesc := "本地文件快速检索工具，基于 Everything 索引毫秒级返回文件名与路径（只读，不读文件内容）。适合定位本地文件：源码、文档、PDF 等。query 支持 Everything 语法（通配符、ext:、dm:、size: 等）。"
		if len(everythingRoots) > 0 {
			fileDesc += fmt.Sprintf("检索范围限定在 %d 个白名单目录内，可用 folder 参数进一步指定其中一个目录。", len(everythingRoots))
		}
		fileDesc += "时间格式可用 time_format 参数自选：datetime（默认年月日时分秒）/ iso / filetime。"
		fileDesc += "默认每页 10 条（max_results 可调，硬上限 20）；命中多时用 page 翻页，别把 max_results 调大。"
		mcp.AddTool(server, &mcp.Tool{
			Name:        "file_search",
			Description: fileDesc,
		}, FileSearch)
		log.Info("Available tool: file_search")
		setState("file_search", "registered")
	} else {
		setState("file_search", "unavailable")
	}

	// ── 注册 pdf_parser 工具（默认关闭） ──
	if conf.PDFParser.Enabled && webfetchInst != nil {
		pdfDesc := "PDF 解析工具，path 为服务进程可见的本地路径、file:// 或远程 http(s) PDF URL（学术结果的 pdf_url 可直接传入）。Docker 仅能读取容器内或已挂载路径，file_search 的宿主机路径不能直接通用。本地优先提取文本，响应标明实际解析器；长文档可用 pages 指定页码（如 '1-10'），省略时默认只解析前 20 页（pdf_parser.max_pages）并提示截断；大文档可存储到临时文件。"
		if conf.PDFParser.MinerUOCREnabled() {
			pdfDesc += "本地读不到文本时回退 MinerU OCR（扫描件/图片型 PDF）。"
		}
		if conf.PDFParser.MinerUToken != "" && conf.PDFParser.MinerURemotePDF {
			pdfDesc += "符合 PDF 地址规则的远程 URL 优先使用 MinerU 精准解析。"
			pdfDesc += "原件超页或源 URL 被 MinerU 拒绝时自动本地裁切所选页分批上传，并实时推送分批进度。"
		}
		pdfDesc += "MinerU 等待可用 wait_seconds 调整（0-1800秒，每任务默认300秒）；返回 task_ref 时可省略 path/pages 查询原任务，默认只查一次，不重新提交。适用边界：仅解析 PDF；网页正文抓取请用 cleanfetch。"
		mcp.AddTool(server, &mcp.Tool{
			Name:        "pdf_parser",
			Description: pdfDesc,
		}, PDFParserHandler)
		log.Info("Available tool: pdf_parser")
		setState("pdf_parser", "registered")
	} else if conf.PDFParser.Enabled {
		setState("pdf_parser", "unavailable")
	}
	// ── 注册 search_history 工具（默认关闭：enabled 显式开启且存储就绪才注册） ──
	// 试验性（idea）：实用性尚未确认，体验确认有价值后再转正式工具。
	if conf.SearchHistoryEnabled() && historyInst != nil {
		histDesc := "本地搜索历史检索工具（试验性，实用性待验证；FTS5 全文匹配历史查询与结果标题/摘要）。省略 query 返回最近历史（按时间倒序）。"
		histDesc += "适合「之前查过什么来着」与复用已查结果而不必重新联网搜索；需要完整正文时对对应 URL 用 cleanfetch。"
		histDesc += "数据只落本地 SQLite、不出网；每次 smartsearch 成功后自动写入（保留期默认 30 天）。"
		mcp.AddTool(server, &mcp.Tool{
			Name:        "search_history",
			Description: histDesc,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool)},
		}, SearchHistoryHandler)
		log.Info("Available tool: search_history")
		setState("search_history", "registered")
	}

	publishTools(states)
}

func RegisterRouter(mux *http.ServeMux, conf config.Config) {
	// MCP Server 惰性创建：NewMCPServer 内的工具注册包含 Everything 探测
	// （ensureEverything），推迟到首个客户端请求时执行，避免 websearch 与
	// Everything 双自启动的时序竞争导致 file_search 在进程周期内缺失。
	var (
		once      sync.Once
		mcpServer *mcp.Server
	)
	getServer := func() *mcp.Server {
		once.Do(func() { mcpServer = NewMCPServer(conf, nil) })
		return mcpServer
	}
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return getServer()
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: 5 * time.Minute,
		// 无状态模式：不校验 Mcp-Session-Id，每个请求用临时会话独立处理，
		// GET（SSE 长连）返回 405。对齐 MCP 2026-07-28 stateless-first 方向。
		Stateless: conf.MCPStateless,
	})
	mux.Handle("/mcp", AuthMiddleware(conf, http.StripPrefix("/mcp", handler)))
}

// AuthMiddleware 业务端点鉴权中间件。
// Authorization: Bearer <token> 或 X-API-Key: <token> 任一通过；token 为空时不鉴权。
func AuthMiddleware(conf config.Config, next http.Handler) http.Handler {
	if conf.AuthToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == conf.AuthToken || r.Header.Get("X-API-Key") == conf.AuthToken {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="websearch"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// RunStdio 在 stdin/stdout 上运行 MCP（JSON-RPC NDJSON）。调用前须完成 Init。
func RunStdio(ctx context.Context, conf config.Config) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return runWithTransport(ctx, conf, &mcp.StdioTransport{}, logger)
}

func runWithTransport(ctx context.Context, conf config.Config, t mcp.Transport, logger *slog.Logger) error {
	opts := &mcp.ServerOptions{}
	if logger != nil {
		opts.Logger = logger
	}
	return NewMCPServer(conf, opts).Run(ctx, t)
}

// buildAcademicToolDescription 动态构建学术搜索工具描述，列出实际可用的引擎。
func buildAcademicToolDescription() string {
	engines := academicSearcher.AcademicEngines()

	// 引擎能力说明
	engineDesc := map[string]string{
		"arxiv":            "arXiv 预印本（CS/物理/数学）",
		"crossref":         "Crossref 学术元数据（全学科，含 DOI/引用）",
		"openalex":         "OpenAlex 开放学术图谱（全学科，含引用数/相关度评分）",
		"semantic_scholar": "Semantic Scholar（CS/AI，含引用数/相关度评分）",
		"pubmed":           "PubMed 生物医学文献（医学/生命科学）",
		"google_scholar":   "Google Scholar（全学科，含引用数/PDF）",
		"europepmc":        "Europe PMC 生物医学/生命科学（PubMed 增补，含引用数）",
		"dblp":             "DBLP 计算机科学文献（CS 会议/期刊索引）",
		"doaj":             "DOAJ 开放获取期刊（全学科 OA 论文）",
	}

	var sb strings.Builder
	sb.WriteString("学术论文检索工具，从多个学术数据库并行搜索论文，返回标准化的 Markdown 格式结果（含标题、作者、DOI、期刊、引用数、PDF 链接）。\n\n")
	sb.WriteString("可用引擎（engines 参数可多选，为空则全部使用）：\n")
	for _, name := range engines {
		desc := engineDesc[name]
		if desc == "" {
			desc = name
		}
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", name, desc))
	}
	sb.WriteString("\n引擎选择建议：医学/生物 → pubmed, europepmc | CS/AI → arxiv, semantic_scholar, dblp | 全学科 → crossref, openalex, google_scholar | 开放获取 → doaj")
	sb.WriteString("\n\n已持有 DOI 或 arXiv id 时，直接将其作为 query（如 '10.1038/s41586-020-2649-2'、'doi:10.1038/s41586-020-2649-2'、'https://doi.org/10.1038/s41586-020-2649-2' 或 '2401.04085'、'arXiv:2401.04085'、'https://arxiv.org/abs/2401.04085'），将走单篇精确查询（遵守已注册/指定 engines；time_range/page 不用于精确查询）；拿到结果中的 pdf_url 后，可将该 URL 作为 pdf_parser 工具的 path 参数解析全文。")
	sb.WriteString("\n\n适用边界：论文、预印本与 DOI 级学术检索；新闻、产品文档等通用网页信息请改用 smartsearch。")
	return sb.String()
}
