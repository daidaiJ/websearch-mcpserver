package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	mcpserver "websearch/mcp"
	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/searxng"
)

func RunServer(conf config.Config) {
	// 在GET /search 接口处理
	mcpserver.Init(conf)
	searxng.Init(conf)
	mux := http.NewServeMux()
	mcpserver.RegisterRouter(mux)
	searxng.RegisterRouter(mux)
	// 3. 优雅启停监听
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", conf.Port),
		Handler: mux,
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 启动服务（异步）
	go func() {
		log.Infof("server start on :%d", conf.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errf("server start failed: %v", err)
			panic(err)
		}
	}()

	// 等待退出信号
	<-quit
	log.Info("shutting down server...")

	// 优雅关闭（5秒超时）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Errf("server shutdown failed: %v", err)
		panic(err)
	}

	log.Info("server exited gracefully")

}

func main() {
	conf, err := config.Load()
	if err != nil {
		panic(err)
	}
	log.NewLogger()
	log.SetLoggerLevel(conf.LogLevel)
	RunServer(*conf)

}
