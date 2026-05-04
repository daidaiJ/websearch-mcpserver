package main

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
	"websearch/searxng"
)

var refCount atomic.Int32
var shutdownCh chan struct{}

func init() {
	shutdownCh = make(chan struct{}, 1)
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

func registerAdminHandlers(mux *http.ServeMux) {
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

		newVal := refCount.Add(int32(req.Delta))
		if newVal < 0 {
			refCount.Store(0)
			newVal = 0
		}

		w.Header().Set("Content-Type", "application/json")
		resp := daemon.RefCountResponse{RefCount: int(newVal)}
		if newVal == 0 {
			resp.Message = "refcount reached zero, server will shutdown gracefully"
			select {
			case shutdownCh <- struct{}{}:
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
		json.NewEncoder(w).Encode(daemon.RefCountResponse{RefCount: int(refCount.Load())})
	}))

	mux.HandleFunc("/__admin/shutdown", localOnlyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"shutdown requested"}`))
		select {
		case shutdownCh <- struct{}{}:
		default:
		}
	}))
}

func RunServer(conf config.Config) {
	if err := mcpserver.Init(conf); err != nil {
		panic(err)
	}
	searxng.Init(conf)
	mux := http.NewServeMux()
	mcpserver.RegisterRouter(mux)
	searxng.RegisterRouter(mux)
	registerAdminHandlers(mux)

	// 启动缓存清理协程
	var cleanup *cache.CleanupScheduler
	if conf.CacheEnabled() && mcpserver.GetCache() != nil {
		cleanup = cache.NewCleanupScheduler(mcpserver.GetCache(), conf.GetCleanupInterval())
		cleanup.Start()
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", conf.Port),
		Handler: mux,
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
	case <-shutdownCh:
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

func runStart(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err == nil && info != nil && daemon.IsRunning(info.PID) {
		// 服务已在运行，增加引用计数
		resp, err := daemon.PostRefCount(info.Port, 1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "server is running (PID %d), but failed to increase refcount: %v\n", info.PID, err)
			os.Exit(1)
		}
		fmt.Printf("server is already running (PID %d), refcount increased to %d\n", info.PID, resp.RefCount)
		return
	}

	// 清理可能残留的 PID 文件
	_ = daemon.RemovePID()

	// 启动新服务，初始引用计数为 1
	refCount.Store(1)
	if err := daemon.WritePID(os.Getpid(), conf.Port); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write PID file: %v\n", err)
		os.Exit(1)
	}
	RunServer(*conf)
}

func runStop(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err != nil || info == nil {
		fmt.Println("server is not running (no PID file)")
		os.Exit(0)
	}

	if !daemon.IsRunning(info.PID) {
		fmt.Printf("server is not running (stale PID file for PID %d)\n", info.PID)
		_ = daemon.RemovePID()
		os.Exit(0)
	}

	resp, err := daemon.PostRefCount(info.Port, -1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to decrease refcount: %v\n", err)
		os.Exit(1)
	}

	if resp.RefCount > 0 {
		fmt.Printf("refcount decreased to %d, server continues running (PID %d)\n", resp.RefCount, info.PID)
		return
	}

	fmt.Printf("refcount reached zero, waiting for server (PID %d) to exit...\n", info.PID)
	if daemon.WaitForExit(info.PID, 10*time.Second) {
		fmt.Println("server exited gracefully")
		_ = daemon.RemovePID()
	} else {
		fmt.Println("timeout waiting for graceful exit, use 'kill' to force stop")
		os.Exit(1)
	}
}

func runKill(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err != nil || info == nil {
		fmt.Println("server is not running (no PID file)")
		os.Exit(0)
	}

	if !daemon.IsRunning(info.PID) {
		fmt.Printf("server is not running (stale PID file for PID %d)\n", info.PID)
		_ = daemon.RemovePID()
		os.Exit(0)
	}

	// 先尝试请求服务端自行退出
	_ = daemon.PostShutdown(info.Port)
	if daemon.WaitForExit(info.PID, 3*time.Second) {
		fmt.Println("server exited gracefully")
		_ = daemon.RemovePID()
		return
	}

	// 强制杀死
	if err := daemon.KillProcess(info.PID); err != nil {
		fmt.Fprintf(os.Stderr, "failed to kill process %d: %v\n", info.PID, err)
		os.Exit(1)
	}
	fmt.Printf("server (PID %d) killed\n", info.PID)
	_ = daemon.RemovePID()
}

func runStatus(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err != nil || info == nil {
		fmt.Println("server status: stopped")
		return
	}

	if !daemon.IsRunning(info.PID) {
		fmt.Printf("server status: stopped (stale PID file for PID %d)\n", info.PID)
		return
	}

	resp, err := daemon.GetStatus(info.Port)
	if err != nil {
		fmt.Printf("server status: running (PID %d, port %d), but failed to query refcount: %v\n", info.PID, info.Port, err)
		return
	}

	fmt.Printf("server status: running (PID %d, port %d, refcount %d)\n", info.PID, info.Port, resp.RefCount)
}

func printUsage() {
	fmt.Println("Usage: websearch-mcpserver <start|stop|kill|status>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start   Start the server or increase refcount if already running")
	fmt.Println("  stop    Decrease refcount, shutdown server when refcount reaches zero")
	fmt.Println("  kill    Force kill the server")
	fmt.Println("  status  Show server status")
}

func main() {
	conf, err := config.Load()
	if err != nil {
		// 对于 stop/kill/status，尝试在无配置时也能执行基本操作
		if len(os.Args) > 1 && (os.Args[1] == "kill" || os.Args[1] == "stop" || os.Args[1] == "status") {
			conf = &config.Config{Port: 8338} // 使用默认端口尝试
		} else {
			fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
			os.Exit(1)
		}
	}
	log.NewLogger()
	log.SetLoggerLevel(conf.LogLevel)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		runStart(conf)
	case "stop":
		runStop(conf)
	case "kill":
		runKill(conf)
	case "status":
		runStatus(conf)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}
