package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func RunServer(port int) {
	// Create an MCP server.
	url := fmt.Sprintf("localhost:%d", port)
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

	log.Printf("MCP server listening on %s", url)
	log.Printf("Available tool: cityTime (cities: nyc, sf, boston)")

	// 2. 手动创建 http.Server 实例（关键：不再直接用 ListenAndServe 快捷方法）
	svc := &http.Server{
		Addr:    url, // 替换为你的监听地址
		Handler: handler,
	}

	// 3. 启动 HTTP 服务（异步执行，避免阻塞主协程）
	go func() {
		log.Printf("Server starting on %s", svc.Addr)
		if err := svc.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// 4. 监听系统退出信号（SIGINT: Ctrl+C，SIGTERM: 容器/系统终止指令）
	quit := make(chan os.Signal, 1)
	// 监听 SIGINT 和 SIGTERM 信号
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	// 阻塞等待退出信号
	<-quit
	log.Println("Received shutdown signal, starting graceful shutdown...")

	// 5. 优雅关闭服务
	// 创建带超时的上下文：给服务 10 秒时间处理完现有请求
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 调用 Shutdown 方法：会停止接收新请求，等待已有请求处理完成后关闭
	if err := svc.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")

}
