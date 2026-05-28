package mcpserver

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"websearch/pkg/config"
	"websearch/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func RegisterRouter(mux *http.ServeMux, conf config.Config) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "websearch server",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		KeepAlive: 30 * time.Second,
	})

	server.AddReceivingMiddleware(createLoggingMiddleware())

	// ── 注册 smartsearch 工具 ──
	if conf.Bing.Enabled {
		searchDesc := "通用网络检索工具，搜索互联网获取最新信息。当需要查询实时数据、最新新闻、技术文档、产品信息、或其他需要联网获取的知识时使用。"
		if conf.LLMEnabled() {
			searchDesc += "支持通过 intent 参数指定搜索意图以获得更精准的结构化摘要。"
		}
		searchDesc += "当主搜索引擎不可用时会自动回退到 Bing 引擎。"

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
		mcp.AddTool(server, &mcp.Tool{
			Name:        "pdf_parser",
			Description: "本地 PDF 解析工具，将 PDF 文件转换为 Markdown。大文档自动存储到临时文件。",
		}, PDFParserHandler)
		log.Info("Available tool: pdf_parser")
	}

	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: 5 * time.Minute,
	})
	mux.Handle("/mcp", http.StripPrefix("/mcp", handler))
}

// buildAcademicToolDescription 动态构建学术搜索工具描述，列出实际可用的引擎。
func buildAcademicToolDescription() string {
	engines := academicSearcher.AcademicEngines()

	// 引擎能力说明
	engineDesc := map[string]string{
		"arxiv":             "arXiv 预印本（CS/物理/数学）",
		"crossref":          "Crossref 学术元数据（全学科，含 DOI/引用）",
		"openalex":          "OpenAlex 开放学术图谱（全学科，含引用数/相关度评分）",
		"semantic_scholar":  "Semantic Scholar（CS/AI，含引用数/相关度评分）",
		"pubmed":            "PubMed 生物医学文献（医学/生命科学）",
		"google_scholar":    "Google Scholar（全学科，含引用数/PDF）",
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
	sb.WriteString("\n引擎选择建议：医学/生物 → pubmed | CS/AI → arxiv, semantic_scholar | 全学科 → crossref, openalex, google_scholar")
	return sb.String()
}
