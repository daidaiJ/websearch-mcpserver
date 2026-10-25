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
	"websearch/pkg/i18n"
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
		// 分段拼接：后续段在 en 目录里自带前导空格（中文无需分隔符、西文需要），
		// 分隔符由翻译数据决定，代码不写语言规则。
		searchDesc := i18n.T("mcp.tool.smartsearch.desc")
		if conf.LLMEnabled() {
			searchDesc += i18n.T("mcp.tool.smartsearch.intent")
		}
		searchDesc += i18n.T("mcp.tool.smartsearch.fetch")
		if fallbackSearch != nil {
			searchDesc += i18n.T("mcp.tool.smartsearch.fallback")
		}
		searchDesc += i18n.T("mcp.tool.smartsearch.scope")

		if conf.LLMEnabled() {
			addTool(server, &mcp.Tool{
				Name:        "smartsearch",
				Description: searchDesc,
			}, WebSearchWithIntent)
			log.InfoT("log.mcp.available_tool_smartsearch_with_intent")
		} else {
			addTool(server, &mcp.Tool{
				Name:        "smartsearch",
				Description: searchDesc,
			}, WebSearchNoIntent)
			log.InfoT("log.mcp.available_tool_smartsearch_no_intent_llm")
		}
		setState("smartsearch", "registered")
	} else {
		setState("smartsearch", "unavailable")
	}

	// ── 注册 academicsearch 工具 ──
	if conf.Academic.Enabled && academicSearcher != nil {
		acadDesc := buildAcademicToolDescription()
		addTool(server, &mcp.Tool{
			Name:        "academicsearch",
			Description: acadDesc,
		}, AcademicSearchHandler)
		log.InfoT("log.mcp.available_tool_academicsearch_engines", "academicEngines", academicSearcher.AcademicEngines())
		setState("academicsearch", "registered")
	} else if conf.Academic.Enabled {
		setState("academicsearch", "unavailable")
	}

	// Saved-body reads remain available even when URL fetching is disabled.
	addTool(server, &mcp.Tool{
		Name:        "cleanfetch",
		Description: i18n.T("mcp.tool.cleanfetch.desc"),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *CleanFetchParams) (*mcp.CallToolResult, any, error) {
		return cleanFetch(ctx, req, params, conf.CleanFetch.Enabled)
	})
	log.InfoT("log.mcp.available_tool_cleanfetch")
	setState("cleanfetch", "registered")

	// ── 注册 file_search 工具（接入时探测通过才暴露，见 ensureEverything） ──
	if ensureEverything() {
		fileDesc := i18n.T("mcp.tool.file_search.desc")
		if len(everythingRoots) > 0 {
			fileDesc += i18n.T("mcp.tool.file_search.roots", "RootCount", len(everythingRoots))
		}
		fileDesc += i18n.T("mcp.tool.file_search.time")
		fileDesc += i18n.T("mcp.tool.file_search.paging")
		addTool(server, &mcp.Tool{
			Name:        "file_search",
			Description: fileDesc,
		}, FileSearch)
		log.InfoT("log.mcp.available_tool_file_search")
		setState("file_search", "registered")
	} else {
		setState("file_search", "unavailable")
	}

	// ── 注册 pdf_parser 工具（默认关闭） ──
	if conf.PDFParser.Enabled && webfetchInst != nil {
		pdfDesc := i18n.T("mcp.tool.pdf_parser.desc")
		if conf.PDFParser.MinerUOCREnabled() {
			pdfDesc += i18n.T("mcp.tool.pdf_parser.ocr")
		}
		if conf.PDFParser.MinerUToken != "" && conf.PDFParser.MinerURemotePDF {
			pdfDesc += i18n.T("mcp.tool.pdf_parser.remote")
			pdfDesc += i18n.T("mcp.tool.pdf_parser.crop")
		}
		pdfDesc += i18n.T("mcp.tool.pdf_parser.wait")
		pdfDesc += i18n.T("mcp.tool.pdf_parser.scope")
		addTool(server, &mcp.Tool{
			Name:        "pdf_parser",
			Description: pdfDesc,
		}, PDFParserHandler)
		log.InfoT("log.mcp.available_tool_pdf_parser")
		setState("pdf_parser", "registered")
	} else if conf.PDFParser.Enabled {
		setState("pdf_parser", "unavailable")
	}
	// ── 注册 search_history 工具（默认关闭：enabled 显式开启且存储就绪才注册） ──
	// 试验性（idea）：实用性尚未确认，体验确认有价值后再转正式工具。
	if conf.SearchHistoryEnabled() && historyInst != nil {
		histDesc := i18n.T("mcp.tool.search_history.desc")
		histDesc += i18n.T("mcp.tool.search_history.use")
		histDesc += i18n.T("mcp.tool.search_history.privacy")
		addTool(server, &mcp.Tool{
			Name:        "search_history",
			Description: histDesc,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool)},
		}, SearchHistoryHandler)
		log.InfoT("log.mcp.available_tool_search_history")
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

// academicEngineDesc 返回单个学术引擎的能力说明；未登记引擎落引擎名本身。
// key 保持字面量调用，便于 pkg/i18n 的目录审计做双向比对。
func academicEngineDesc(name string) string {
	switch name {
	case "arxiv":
		return i18n.T("mcp.tool.academicsearch.engine.arxiv")
	case "crossref":
		return i18n.T("mcp.tool.academicsearch.engine.crossref")
	case "openalex":
		return i18n.T("mcp.tool.academicsearch.engine.openalex")
	case "semantic_scholar":
		return i18n.T("mcp.tool.academicsearch.engine.semantic_scholar")
	case "pubmed":
		return i18n.T("mcp.tool.academicsearch.engine.pubmed")
	case "google_scholar":
		return i18n.T("mcp.tool.academicsearch.engine.google_scholar")
	case "europepmc":
		return i18n.T("mcp.tool.academicsearch.engine.europepmc")
	case "dblp":
		return i18n.T("mcp.tool.academicsearch.engine.dblp")
	case "doaj":
		return i18n.T("mcp.tool.academicsearch.engine.doaj")
	default:
		return name
	}
}

// buildAcademicToolDescription 动态构建学术搜索工具描述，列出实际可用的引擎。
func buildAcademicToolDescription() string {
	engines := academicSearcher.AcademicEngines()

	var sb strings.Builder
	sb.WriteString(i18n.T("mcp.tool.academicsearch.desc"))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.T("mcp.tool.academicsearch.engines"))
	sb.WriteString("\n")
	for _, name := range engines {
		fmt.Fprintf(&sb, "  - %s: %s\n", name, academicEngineDesc(name))
	}
	sb.WriteString("\n")
	sb.WriteString(i18n.T("mcp.tool.academicsearch.advice"))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.T("mcp.tool.academicsearch.doi"))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.T("mcp.tool.academicsearch.scope"))
	return sb.String()
}
