package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	mcpserver "websearch/mcp"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/daemon"
	"websearch/pkg/dashboard"
	"websearch/pkg/log"
	"websearch/pkg/quota"
	"websearch/pkg/telemetry"
)

// Server 封装了 MCP 服务的生命周期管理。
// 可独立使用，也可被外部项目嵌入。
type Server struct {
	refMu         sync.Mutex // 保护 refCount 的「检查 + 更新」复合操作
	refCount      int32
	shutdownCh    chan struct{}
	startedAt     time.Time
	inflight      atomic.Int64 // 正在处理的 HTTP 请求数（进程内指标）
	embeddedMu    sync.Mutex
	embedded      http.Handler
	closeEmbedded func() error
}

// New 创建一个新的 Server 实例。
func New() *Server {
	return &Server{
		shutdownCh: make(chan struct{}, 1),
		startedAt:  time.Now(),
	}
}

// instrument 统计正在处理的 HTTP 请求数，供 /metrics 的进程内 gauge 使用。
func (s *Server) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.inflight.Add(1)
		defer s.inflight.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// SetRefCount 设置初始引用计数（通常在首次启动时调用，设为 1）。
func (s *Server) SetRefCount(n int32) {
	s.refMu.Lock()
	defer s.refMu.Unlock()
	s.refCount = max(n, 0)
}

// RefCount 返回当前引用计数。
func (s *Server) RefCount() int32 {
	s.refMu.Lock()
	defer s.refMu.Unlock()
	return s.refCount
}

// applyRefDelta 在锁内完成「检查 + 更新」，返回新的引用计数与是否需要触发关闭。
// 计数按饱和处理（下限 0），因此重复 stop 是真正的 no-op：计数已为 0 时再减
// 不再产生新的 1→0 迁移，也就不会重复发关闭信号（B5）。
func (s *Server) applyRefDelta(delta int32) (int32, bool) {
	s.refMu.Lock()
	defer s.refMu.Unlock()
	if s.refCount == 0 && delta <= 0 {
		return 0, false
	}
	next := s.refCount + delta
	if next < 0 {
		next = 0
	}
	tripped := s.refCount > 0 && next == 0
	s.refCount = next
	return next, tripped
}

// requestShutdown 非阻塞发一次关闭信号（重复请求被容量 1 的通道吸收）。
func (s *Server) requestShutdown() {
	select {
	case s.shutdownCh <- struct{}{}:
	default:
	}
}

// localOnlyMiddleware 限制 admin 接口仅本地访问
func localOnlyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !dashboard.IsLoopbackRemote(r) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// dashboardAccessMiddleware 控制台访问边界：loopback 始终放行；其它来源仅在
// dashboard.allowed_networks 显式声明的网段内放行（默认空 = 仅本机）。
// Host 头不参与判定：它可被任意伪造，与连接的真实来源无关。
// 网段配置非法时返回错误，调用方应回退为仅本机（fail-closed）。
func dashboardAccessMiddleware(conf config.Config) (func(http.HandlerFunc) http.HandlerFunc, error) {
	nets, err := conf.Dashboard.ParseAllowedNetworks()
	if err != nil {
		return nil, err
	}
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if dashboard.IsLoopbackRemote(r) {
				next(w, r)
				return
			}
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			if ip := net.ParseIP(host); ip != nil {
				for _, n := range nets {
					if n.Contains(ip) {
						next(w, r)
						return
					}
				}
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
	}, nil
}

func (s *Server) registerAdminHandlers(mux *http.ServeMux, conf config.Config, metrics *telemetry.Store) {
	s.registerAdminHandlersWithQuota(mux, conf, metrics, quota.New(conf, metrics))
}

func (s *Server) registerAdminHandlersWithQuota(mux *http.ServeMux, conf config.Config, metrics *telemetry.Store, service *quota.Service) {
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

		newVal, tripped := s.applyRefDelta(int32(req.Delta))

		w.Header().Set("Content-Type", "application/json")
		resp := daemon.RefCountResponse{RefCount: int(newVal)}
		if tripped {
			resp.Message = "refcount reached zero, server will shutdown gracefully"
			s.requestShutdown()
		}
		json.NewEncoder(w).Encode(resp)
	}))

	mux.HandleFunc("/__admin/status", localOnlyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.RefCountResponse{RefCount: int(s.RefCount())})
	}))

	mux.HandleFunc("/__admin/shutdown", localOnlyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"shutdown requested"}`))
		s.requestShutdown()
	}))

	mux.HandleFunc("/__admin/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(daemon.RefCountResponse{
			RefCount: int(s.RefCount()),
			Message:  "running",
		})
	})

	if conf.Dashboard.Enabled {
		guard, err := dashboardAccessMiddleware(conf)
		if err != nil {
			log.ErrorT("log.server.invalid_dashboard_allowed_networks_dashboard_falls", "err", err)
			guard = localOnlyMiddleware
		}
		h := dashboard.NewWithQuota(conf, metrics, mcpserver.GetCache(), func() {
			select {
			case s.shutdownCh <- struct{}{}:
			default:
			}
		}, service)
		h.SetSearchHistory(mcpserver.GetSearchHistory())
		h.RegisterWithReadGuard(mux, guard)
	}
}

func (s *Server) runtimeHandler(conf config.Config) (http.Handler, func() error, error) {
	var metrics *telemetry.Store
	if conf.Dashboard.Enabled || conf.Dashboard.Quotas.HasSearchEnforcement() {
		var err error
		metrics, err = telemetry.OpenWithMaxOpenConns(conf.GetDashboardStoragePath(), conf.Dashboard.RetentionDays, conf.Dashboard.GetMaxOpenConns())
		if err != nil {
			return nil, nil, fmt.Errorf("initialize dashboard telemetry: %w", err)
		}
	}
	var stopTelemetry func()
	if conf.Dashboard.Enabled {
		if p := config.GetDashboardOverlayFile(); p != "" {
			log.InfoT("log.server.dashboard_standalone_config", "p", p)
		}
		if !conf.Dashboard.AdminPasswordConfigured() {
			log.WarnT("log.server.admin_password_not_configured_dashboard_admin")
		}
		telemetry.SetDefault(metrics)
		if policy, err := dashboard.SuspensionPolicy(conf.Dashboard.Suspension); err == nil {
			telemetry.SetSuspensionPolicy(policy)
		}
	}
	if metrics != nil {
		// 明细按 retention_days 滚动过期：启动清一次，此后每 6 小时滚一次。
		// 仅启用额度强制、未开控制中心时也要清理，否则遥测库会无界增长。
		stopTelemetry = metrics.StartCleanupLoop(6 * time.Hour)
		_ = metrics.Cleanup()
	}
	var service *quota.Service
	if metrics != nil {
		service = quota.New(conf, metrics)
	}
	if err := mcpserver.Init(conf,
		mcpserver.WithSearchEngineQuota(conf, service),
		mcpserver.WithSummarizer(conf),
		mcpserver.WithCache(conf),
		mcpserver.WithWebFetch(conf),
		mcpserver.WithMCPHubFetch(conf),
		mcpserver.WithJinaReader(conf),
		mcpserver.WithEverything(conf),
		mcpserver.WithSearchHistory(conf),
	); err != nil {
		if stopTelemetry != nil {
			stopTelemetry()
		}
		if metrics != nil {
			_ = metrics.Close()
		}
		return nil, nil, err
	}
	mux := http.NewServeMux()
	mcpserver.RegisterRouter(mux, conf)
	s.registerAdminHandlersWithQuota(mux, conf, metrics, service)
	// Quota-only mode keeps the durable ledger available to search without exporting
	// dashboard telemetry.
	var stopWarmup func()
	if conf.Dashboard.Enabled {
		s.registerMetricsHandler(mux, metrics)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer cancel()
			_ = service.Get(ctx)
		}()
		stopWarmup = func() {
			cancel()
			<-done
		}
	} else {
		s.registerMetricsHandler(mux, nil)
	}

	// 启动缓存清理协程
	var cleanup *cache.CleanupScheduler
	if conf.CacheEnabled() && mcpserver.GetCache() != nil {
		cleanup = cache.NewCleanupScheduler(mcpserver.GetCache(), conf.GetCleanupInterval())
		cleanup.Start()
	}
	closeRuntime := func() error {
		if cleanup != nil {
			cleanup.Stop(context.Background())
		}
		if stopWarmup != nil {
			stopWarmup()
		}
		if stopTelemetry != nil {
			stopTelemetry()
		}
		err := mcpserver.CloseRuntimeResources()
		if metrics != nil {
			err = errors.Join(err, metrics.Close())
		}
		return err
	}
	return s.instrument(mux), closeRuntime, nil
}

// Run 启动 HTTP 服务并阻塞直到收到关闭信号或引用计数归零。
// 监听成功（端口可用）后调用 onListening 回调（可用于写 PID 文件）；
// 监听失败（如端口占用）返回错误，不 panic。
// 若需把路由挂载到宿主 HTTP 服务，使用 Handler。
func (s *Server) Run(conf config.Config, onListening ...func()) (err error) {
	handler, closeRuntime, err := s.runtimeHandler(conf)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeRuntime()) }()

	host := conf.Host
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(conf.Port))
	if (host == "0.0.0.0" || host == "::") && conf.AuthToken == "" {
		log.ErrorT("log.server.listening_address_is_open_on_all", "addr", addr)
	}

	// 先监听成功再写 PID / Serve，避免端口占用时留下脏 PID 文件
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s failed: %w", addr, err)
	}
	for _, cb := range onListening {
		cb()
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	go func() {
		log.InfoT("log.server.server_start_on", "addr", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.ErrorT("log.server.server_start_failed", "err", err)
			panic(err)
		}
	}()

	// 等待关闭信号（信号或引用计数归零）
	select {
	case sig := <-quit:
		log.InfoT("log.server.received_signal", "sig", sig)
	case <-s.shutdownCh:
		log.InfoT("log.server.refcount_reached_zero_initiating_graceful_shutdown")
	}

	log.InfoT("log.server.shutting_down_server")

	// 优雅关闭（5秒超时）：先停 HTTP，再进行中请求结束后再关依赖
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.ErrorT("log.server.server_shutdown_failed", "err", err)
		_ = srv.Close()
		return err
	}

	// 清理 PID 文件
	_ = daemon.RemovePID()

	log.InfoT("log.server.server_exited_gracefully")
	return nil
}

// Handler 返回可挂载的 MCP 和 admin 路由。由调用方管理 HTTP 服务，
// 停止接收请求并排空后，调用 Close 释放本进程的 MCP 资源。
// 同一个 Server 在关闭前复用首次创建的 Handler。
func (s *Server) Handler(conf config.Config) http.Handler {
	s.embeddedMu.Lock()
	defer s.embeddedMu.Unlock()
	if s.embedded == nil {
		handler, closeRuntime, err := s.runtimeHandler(conf)
		if err != nil {
			panic(err)
		}
		s.embedded, s.closeEmbedded = handler, closeRuntime
	}
	return s.embedded
}

// Close closes an embedded Handler's resources after its HTTP host has drained.
// It is safe to call more than once. Run manages its own shutdown.
func (s *Server) Close() error {
	s.embeddedMu.Lock()
	defer s.embeddedMu.Unlock()
	if s.closeEmbedded == nil {
		return nil
	}
	err := s.closeEmbedded()
	s.embedded, s.closeEmbedded = nil, nil
	return err
}
