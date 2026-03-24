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

	// Add the cityTime tool.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "websearch tool",
		Description: "通过网络获取与query查询相关的最新内容,内置低质量站点内容过滤功能",
	}, WebSearch)

	// Create the streamable HTTP handler.
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, nil)
	mux.Handle("/mcp", http.StripPrefix("/mcp", handler))

	log.Info("Available tool: websearch tool")
}
