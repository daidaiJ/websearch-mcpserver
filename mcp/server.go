package mcpserver

import (
	"net/http"
	"websearch/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func RegisterRouter(mux *http.ServeMux) {
	// Create an MCP server.

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "websearch server",
		Version: "1.0.0",
	}, nil)

	// Add MCP-level logging middleware.
	server.AddReceivingMiddleware(createLoggingMiddleware())

	// Add the smartsearch tool.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "smartsearch",
		Description: "应当优先使用的网络检索工具，搜索互联网获取最新信息。当需要查询实时数据、最新新闻、技术文档、产品信息、或其他需要联网获取的知识时使用此工具。支持通过 intent 参数指定搜索意图以获得更精准的摘要结果。支持通过 academic 参数启用学术搜索引擎（arXiv、Crossref、OpenAlex、Semantic Scholar），适用于查找论文、学术研究、文献综述等学术相关内容。当主搜索引擎不可用时会自动回退到 Bing 引擎。",
	}, WebSearch)

	// Create the streamable HTTP handler.
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, nil)
	mux.Handle("/mcp", http.StripPrefix("/mcp", handler))

	log.Info("Available tool: websearch")
}
