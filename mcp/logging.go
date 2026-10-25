package mcpserver

import (
	"context"
	"time"
	"websearch/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createLoggingMiddleware creates an MCP middleware that logs method calls.
func createLoggingMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(
			ctx context.Context,
			method string,
			req mcp.Request,
		) (mcp.Result, error) {
			start := time.Now()
			sessionID := req.GetSession().ID()

			// Log request details.
			log.InfoT("log.mcp.request_session_method", "sessionID", sessionID, "method", method)

			// Call the actual handler.
			result, err := next(ctx, method, req)

			// Log response details.
			duration := time.Since(start)

			if err != nil {
				log.InfoT("log.mcp.response_session_method_status_error_duration", "sessionID", sessionID, "method", method, "duration", duration, "err", err)
			} else {
				log.InfoT("log.mcp.response_session_method_status_ok_duration", "sessionID", sessionID, "method", method, "duration", duration)
			}

			return result, err
		}
	}
}
