package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	mcpserver "websearch/mcp"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/daemon"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/searxng"
)

// Server 封装了 MCP 服务的生命周期管理。
// 可独立使用，也可被外部项目嵌入。
type Server struct {
	refCount   atomic.Int32
	shutdownCh chan struct{}
}

// New 创建一个新的 Server 实例。
func New() *Server {
	return &Server{
		shutdownCh: make(chan struct{}, 1),
	}
}

// SetRefCount 设置初始引用计数。
// 通常在首次启动时调用，设为 1。
func (s *Server) SetRefCount(n int32) {
	s.refCount.Store(n)
}

// RefCount 返回当前引用计数。
func (s *Server) RefCount() int32 {
	return s.refCount.Load()
}

// localOnlyMiddleware 限制 admin 接口仅本地访问
func localOnlyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) registerAdminHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/__admin/refcount", localOnlyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Delta int `json:"delta"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		newVal := s.refCount.Add(int32(req.Delta))
		if newVal < 0 {
			s.refCount.Store(0)
			newVal = 0
		}

		w.Header().Set("Content-Type", "application/json")
		resp := daemon.RefCountResponse{RefCount: int(newVal)}
		if newVal == 0 {
			resp.Message = "refcount reached zero, server will shutdown gracefully"
			select {
			case s.shutdownCh <- struct{}{}:
			default:
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))

	mux.HandleFunc("/__admin/status", localOnlyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(daemon.RefCountResponse{RefCount: int(s.refCount.Load())})
	}))

	mux.HandleFunc("/__admin/shutdown", localOnlyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"shutdown requested"}`))
		select {
		case s.shutdownCh <- struct{}{}:
		default:
		}
	}))

	mux.HandleFunc("/__admin/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(daemon.RefCountResponse{
			RefCount: int(s.refCount.Load()),
			Message:  "running",
		})
	})
}

// Run 启动 HTTP 服务并阻塞直到收到关闭信号或引用计数归零。
// 外部项目可直接调用此方法将 MCP 服务嵌入到自己的 HTTP Server 中。
func (s *Server) Run(conf config.Config) {
	if err := mcpserver.Init(conf,
		mcpserver.WithSearchEngine(conf),
		mcpserver.WithSummarizer(conf),
		mcpserver.WithCache(conf),
		mcpserver.WithWebFetch(conf),
		mcpserver.WithJinaReader(conf),
	); err != nil {
		panic(err)
	}
	searchGroup, _ := search.NewFromConfig(conf)
	searxng.Init(searchGroup)
	mux := http.NewServeMux()
	mcpserver.RegisterRouter(mux, conf)
	searxng.RegisterRouter(mux)
	s.registerAdminHandlers(mux)

	// 启动缓存清理协程
	var cleanup *cache.CleanupScheduler
	if conf.CacheEnabled() && mcpserver.GetCache() != nil {
		cleanup = cache.NewCleanupScheduler(mcpserver.GetCache(), conf.GetCleanupInterval())
		cleanup.Start()
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", conf.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Infof("server start on :%d", conf.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errf("server start failed: %v", err)
			panic(err)
		}
	}()

	// 等待关闭信号（信号或引用计数归零）
	select {
	case sig := <-quit:
		log.Infof("received signal: %v", sig)
	case <-s.shutdownCh:
		log.Info("refcount reached zero, initiating graceful shutdown")
	}

	log.Info("shutting down server...")

	// 停止缓存清理协程
	if cleanup != nil {
		cleanup.Stop(context.Background())
	}

	// 关闭缓存数据库
	if c := mcpserver.GetCache(); c != nil {
		c.Close()
	}

	// 关闭 WebFetch 引擎
	if wf := mcpserver.GetWebFetch(); wf != nil {
		wf.Close()
	}

	// 优雅关闭（5秒超时）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Errf("server shutdown failed: %v", err)
		panic(err)
	}

	// 清理 PID 文件
	_ = daemon.RemovePID()

	log.Info("server exited gracefully")
}

// Handler 返回注册了 MCP、SearXNG 和 admin 路由的 http.Handler。
// 适用于需要自行管理 http.Server 生命周期的嵌入场景。
func (s *Server) Handler(conf config.Config) http.Handler {
	if err := mcpserver.Init(conf,
		mcpserver.WithSearchEngine(conf),
		mcpserver.WithSummarizer(conf),
		mcpserver.WithCache(conf),
		mcpserver.WithWebFetch(conf),
		mcpserver.WithJinaReader(conf),
	); err != nil {
		panic(err)
	}
	searchGroup, _ := search.NewFromConfig(conf)
	searxng.Init(searchGroup)
	mux := http.NewServeMux()
	mcpserver.RegisterRouter(mux, conf)
	searxng.RegisterRouter(mux)
	s.registerAdminHandlers(mux)
	return mux
}
