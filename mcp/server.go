package mcpserver

import (
	"net/http"
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
	if conf.LLMEnabled() {
		// LLM 启用：注册含 intent 参数的版本
		mcp.AddTool(server, &mcp.Tool{
			Name:        "smartsearch",
			Description: "应当优先使用的网络检索工具，搜索互联网获取最新信息。当需要查询实时数据、最新新闻、技术文档、产品信息、或其他需要联网获取的知识时使用此工具。支持通过 intent 参数指定搜索意图以获得更精准的摘要结果。支持通过 academic 参数启用学术搜索引擎（arXiv、Crossref、OpenAlex、Semantic Scholar），适用于查找论文、学术研究、文献综述等学术相关内容。当主搜索引擎不可用时会自动回退到 Bing 引擎。",
		}, WebSearchWithIntent)
		log.Info("Available tool: smartsearch (with intent)")
	} else {
		// LLM 未启用：注册不含 intent 参数的版本，节省上下文 token
		mcp.AddTool(server, &mcp.Tool{
			Name:        "smartsearch",
			Description: "应当优先使用的网络检索工具，搜索互联网获取最新信息。当需要查询实时数据、最新新闻、技术文档、产品信息、或其他需要联网获取的知识时使用此工具。支持通过 academic 参数启用学术搜索引擎（arXiv、Crossref、OpenAlex、Semantic Scholar），适用于查找论文、学术研究、文献综述等学术相关内容。当主搜索引擎不可用时会自动回退到 Bing 引擎。",
		}, WebSearchNoIntent)
		log.Info("Available tool: smartsearch (no intent, LLM disabled)")
	}

	// ── 注册 cleanfetch 工具（仅当 Jina Reader 可用时） ──
	if jinaInst != nil {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "cleanfetch",
			Description: "网页内容抓取工具，通过外部 API 接口获取指定 URL 的干净网页内容，减小被网站防爬机制阻断的风险。适用于需要阅读某篇文章、获取网页正文、或提取特定页面信息的场景。返回 Markdown 格式的清理后内容。",
		}, CleanFetch)
		log.Info("Available tool: cleanfetch")
	}

	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: 5 * time.Minute,
	})
	mux.Handle("/mcp", http.StripPrefix("/mcp", handler))
}
