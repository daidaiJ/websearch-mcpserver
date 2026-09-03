package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
		Version: "1.0.0",
	}, opts)

	server.AddReceivingMiddleware(createLoggingMiddleware())
	registerTools(server, conf)
	return server
}

func registerTools(server *mcp.Server, conf config.Config) {
	// ── 注册 smartsearch 工具 ──
	if conf.Bing.Enabled {
		searchDesc := "通用联网检索工具，获取实时信息（新闻/技术文档/产品/数据等）。查询词需精准凝练、聚焦核心意图，避免堆砌大量同义/次要词形成关键词列表（会稀释相关性）。"
		if conf.LLMEnabled() {
			searchDesc += "可用 intent 参数说明检索目的以获得更精准的结构化摘要。"
		}
		searchDesc += "主引擎不可用时自动回退 Bing。"

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
	}

	// ── 注册 academicsearch 工具 ──
	if conf.Academic.Enabled && academicSearcher != nil {
		acadDesc := buildAcademicToolDescription()
		mcp.AddTool(server, &mcp.Tool{
			Name:        "academicsearch",
			Description: acadDesc,
		}, AcademicSearchHandler)
		log.Infof("Available tool: academicsearch (engines: %v)", academicSearcher.AcademicEngines())
	}

	// ── 注册 cleanfetch 工具（需显式启用） ──
	if conf.CleanFetch.Enabled {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "cleanfetch",
			Description: "网页内容抓取工具，获取指定 URL 的干净 Markdown 内容。",
		}, CleanFetch)
		log.Info("Available tool: cleanfetch")
	}

	// ── 注册 pdf_parser 工具（默认关闭） ──
	if conf.PDFParser.Enabled && webfetchInst != nil {
		pdfDesc := "本地 PDF 解析工具，优先用 PDF 库提取文本转为 Markdown；大文档自动存储到临时文件。"
		if conf.PDFParser.MinerUOCREnabled() {
			pdfDesc += "本地读不到文本时回退 MinerU OCR（扫描件/图片型 PDF）。"
		} else if conf.PDFParser.MinerUToken != "" {
			pdfDesc += "已配置 MinerU Token（远程 URL 可用精准解析）。"
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "pdf_parser",
			Description: pdfDesc,
		}, PDFParserHandler)
		log.Info("Available tool: pdf_parser")
	}
}

func RegisterRouter(mux *http.ServeMux, conf config.Config) {
	server := NewMCPServer(conf, nil)
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
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
	return sb.String()
}
