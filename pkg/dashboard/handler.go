package dashboard

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/quota"
	"websearch/pkg/telemetry"
)

// secureEquals 常量时间字符串比较；供管理员用户名/口令校验共用。
func secureEquals(input, want string) bool {
	return input != "" && subtle.ConstantTimeCompare([]byte(input), []byte(want)) == 1
}

//go:embed web/*
var webFiles embed.FS

const defaultEventLimit = 50

type Handler struct {
	conf    config.Config
	store   *telemetry.Store
	quota   *quota.Service
	cache   *cache.Cache
	restart func()
}

func New(conf config.Config, store *telemetry.Store, searchCache *cache.Cache, restart func()) *Handler {
	return &Handler{conf: conf, store: store, quota: quota.New(conf, store), cache: searchCache, restart: restart}
}

// IsLoopbackRemote 判定请求是否来自本机 loopback。
// 供控制台访问边界与写操作保护共用；Host 头不参与判定。
func IsLoopbackRemote(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// authorizeWrite 是所有写端点的统一闸门：
//  1. 默认仅 loopback；write_allowed_networks 可放行用户指定的 CIDR/IP，
//     读白名单不会授予写权限，不信任 X-Forwarded-* / X-Real-IP；
//  2. 管理员口令 —— 必须在 dashboard.yaml 显式配置 dashboard.admin_password
//     （或 admin_password_sha256），经 X-Admin-Password 头随请求提交。
//     该口令不在设置页白名单、不写入 secrets 覆盖文件，即 WebUI 无法读取
//     或修改它；未配置口令时写端点整体禁用（安全的默认）。
//  3. 管理员用户名 —— 可选：仅当 dashboard.admin_username 显式配置时才要求
//     X-Admin-User 头匹配（常量时间比较）；未配置 = 与口令-only 行为一致，
//     不做任何用户名检查。
func (h *Handler) authorizeWrite(w http.ResponseWriter, r *http.Request) bool {
	if allowed, reason := h.writeAccess(r); !allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": reason})
		return false
	}
	if !h.conf.Dashboard.AdminPasswordConfigured() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.write.no_password_restart")})
		return false
	}
	if user := h.conf.Dashboard.AdminUsername; user != "" {
		if !secureEquals(r.Header.Get("X-Admin-User"), user) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.write.bad_user")})
			return false
		}
	}
	if !h.conf.Dashboard.VerifyAdminPassword(r.Header.Get("X-Admin-Password")) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.write.bad_password")})
		return false
	}
	return true
}

// Register 挂载控制中心路由。所有端点都经 i18n.HTTP 注入按 Accept-Language
// 协商出的 Localizer（浏览器语言能被满足就用浏览器语言，否则回退 config.language），
// handler 内用 i18n.FromContext(r.Context()) 取用。
func (h *Handler) Register(mux *http.ServeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	assets, _ := fs.Sub(webFiles, "web")
	fileServer := http.FileServer(http.FS(assets))
	// 内嵌资源禁止启发式缓存：二进制升级后浏览器必须立即取到新资源
	mux.Handle("/dashboard/", guard(i18n.HTTP(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if r.URL.Path == "/dashboard/" {
			serveIndex(w, r, assets)
			return
		}
		http.StripPrefix("/dashboard/", fileServer).ServeHTTP(w, r)
	})))
	mux.HandleFunc("/dashboard", guard(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusTemporaryRedirect)
	}))
	mux.HandleFunc("/__admin/api/overview", guard(i18n.HTTP(h.overview)))
	mux.HandleFunc("/__admin/api/events", guard(i18n.HTTP(h.events)))
	mux.HandleFunc("/__admin/api/calls", guard(i18n.HTTP(h.calls)))
	mux.HandleFunc("/__admin/api/request-events", guard(i18n.HTTP(h.requestEvents)))
	mux.HandleFunc("/__admin/api/quotas", guard(i18n.HTTP(h.quotas)))
	mux.HandleFunc("/__admin/api/quotas/reset", guard(i18n.HTTP(h.quotaReset)))
	mux.HandleFunc("/__admin/api/quotas/adjust", guard(i18n.HTTP(h.quotaAdjust)))
	mux.HandleFunc("/__admin/api/brand/logo", guard(i18n.HTTP(h.brandLogo)))
	mux.HandleFunc("/__admin/api/clients", guard(i18n.HTTP(h.clients)))
	mux.HandleFunc("/__admin/api/providers", guard(i18n.HTTP(h.providers)))
	mux.HandleFunc("/__admin/api/metrics", guard(i18n.HTTP(h.metricsHandler)))
	mux.HandleFunc("/__admin/api/settings", guard(i18n.HTTP(h.settings)))
	mux.HandleFunc("/__admin/api/secrets", guard(i18n.HTTP(h.secrets)))
	mux.HandleFunc("/__admin/api/restart", guard(i18n.HTTP(h.restartService)))
	mux.HandleFunc("/__admin/api/cache/clear", guard(i18n.HTTP(h.clearCache)))
}

// indexLangAnchor 是 index.html 中 <html lang> 的取值锚点（见 serveIndex）。
const indexLangAnchor = `lang="zh-CN"`

// indexMessagesAnchor 是 index.html 里内联前端文案的占位注释（见 serveIndex）。
const indexMessagesAnchor = "<!--i18n-messages-->"

// serveIndex 输出内嵌 index.html，并把 <html lang> 改写为本次请求协商出的语言。
// 前端据 document.documentElement.lang 选词典并内联当前语言的 web 域文案，
// 首帧即正确语言，无需二次切换。
func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}
	code := i18n.FromContext(r.Context()).Code()
	page = bytes.Replace(page, []byte(indexLangAnchor), []byte(`lang="`+htmlLang(code)+`"`), 1)
	page = bytes.Replace(page, []byte(indexMessagesAnchor),
		[]byte(`<script>window.__I18N__ = `+string(i18n.RawMessages(code, "web"))+`;</script>`), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

// htmlLang 把语言代码转成 <html lang> 取值：中文带地区（zh-CN），英文用裸 en。
func htmlLang(code string) string {
	if code == i18n.En {
		return "en"
	}
	return "zh-CN"
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "dashboard telemetry disabled"})
		return
	}
	observed, err := h.store.Overview()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, buildDashboardOverview(h.conf, observed, i18n.FromContext(r.Context())))
}

// eventLimit accepts the dashboard page sizes 20/50/100; any other value
// (including a missing or unparsable one) falls back to 50.
func eventLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return defaultEventLimit
	}
	switch n {
	case 20, 50, 100:
		return n
	}
	return defaultEventLimit
}

// eventErrorKind accepts one of the stable telemetry error kinds; empty or
// unknown values mean "all".
func eventErrorKind(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "rate_limit", "captcha", "access_denied", "timeout", "parse", "no_result", "network", "unknown":
		return value
	default:
		return ""
	}
}

// eventKind accepts kind=provider|tool|none; empty or unknown values mean
// "all". "none" is the explicit no-match sentinel used when the dashboard
// combines a tool filter with a provider filter.
func eventKind(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "provider" || value == "tool" || value == "none" {
		return value
	}
	return ""
}

// eventStatus accepts status=all|success|failure; empty or unknown values
// mean "all".
func eventStatus(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "success" || value == "failure" {
		return value
	}
	return ""
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "dashboard telemetry disabled"})
		return
	}
	query := r.URL.Query()
	kind := eventKind(query.Get("kind"))
	if kind == "none" {
		writeJSON(w, http.StatusOK, []telemetry.StoredEvent{})
		return
	}
	out, err := h.store.RecentFiltered(telemetry.EventFilter{
		Kind:      kind,
		Status:    eventStatus(query.Get("status")),
		Source:    strings.TrimSpace(query.Get("source")),
		ErrorKind: eventErrorKind(query.Get("error_kind")),
		RequestID: strings.TrimSpace(query.Get("request_id")),
		Limit:     eventLimit(query.Get("limit")),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	h.attributeEvents(out)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) calls(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "dashboard telemetry disabled"})
		return
	}
	query := r.URL.Query()
	limit := query.Get("limit")
	if limit == "" {
		limit = "100"
	}
	out, err := h.store.RecentCalls(telemetry.CallFilter{
		Tool: strings.TrimSpace(query.Get("tool")), Provider: strings.TrimSpace(query.Get("provider")),
		Status: eventStatus(query.Get("status")), ErrorKind: eventErrorKind(query.Get("error_kind")),
		Limit: eventLimit(limit),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	h.attributeEvents(out)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) requestEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "dashboard telemetry disabled"})
		return
	}
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id is required"})
		return
	}
	out, err := h.store.RequestEvents(requestID)
	if err != nil {
		writeError(w, err)
		return
	}
	h.attributeEvents(out)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) projectForClient(client, project string) string {
	if project != "" {
		return project
	}
	return telemetry.NormalizeProject(h.conf.Dashboard.ClientProjects[strings.ToLower(strings.TrimSpace(client))])
}

func (h *Handler) attributeEvents(rows []telemetry.StoredEvent) {
	for i := range rows {
		rows[i].Application = telemetry.Application(rows[i].Client)
		rows[i].Project = h.projectForClient(rows[i].Client, rows[i].Project)
	}
}

// providers returns the merged provider state, including the read-only
// circuit breaker fields. The dashboard overview already includes the same
// data; this endpoint keeps a stable machine-readable surface for scripts.
func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "dashboard telemetry disabled"})
		return
	}
	observed, err := h.store.Overview()
	if err != nil {
		writeError(w, err)
		return
	}
	view := buildDashboardOverview(h.conf, observed, i18n.FromContext(r.Context()))
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": view.GeneratedAt,
		"summary":      view.System,
		"providers":    append(append([]SourceView{}, view.Sources...), view.AcademicSources...),
	})
}

func (h *Handler) quotas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, h.quota.Get(ctx))
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out := settingsFileView(h.conf, config.GetConfigFile(), i18n.FromContext(r.Context()))
		allowed, reason := h.writeAccess(r)
		if out["config_error"] != nil {
			allowed, reason = false, i18n.TCtx(r.Context(), "dashboard.settings.unreadable")
		}
		out["write_access"] = map[string]any{"allowed": allowed, "reason": reason}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		if !h.authorizeWrite(w, r) {
			return
		}
		var req SettingsRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		out, err := applySettingsFile(h.conf, req, config.GetConfigFile(), func() error {
			if h.cache == nil {
				return nil
			}
			_, err := h.cache.Clear()
			return err
		}, i18n.FromContext(r.Context()))
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errSettingsConflict) {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) writeAccess(r *http.Request) (bool, string) {
	if !h.conf.Dashboard.AdminPasswordConfigured() {
		return false, i18n.TCtx(r.Context(), "dashboard.write.no_password")
	}
	if IsLoopbackRemote(r) {
		return true, ""
	}
	networks, err := h.conf.Dashboard.ParseWriteAllowedNetworks()
	if err != nil {
		return false, i18n.TCtx(r.Context(), "dashboard.write.bad_allowlist")
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil {
		for _, network := range networks {
			if network.Contains(ip) {
				return true, ""
			}
		}
	}
	return false, i18n.TCtx(r.Context(), "dashboard.write.readonly")
}

func (h *Handler) secrets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}
	var req SecretRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := saveSecret(h.conf, req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "restart_required": true})
}

func (h *Handler) restartService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "confirm=true required"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restarting": true})
	if h.restart != nil {
		go func() { time.Sleep(350 * time.Millisecond); h.restart() }()
	}
}

func (h *Handler) clearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "confirm=true required"})
		return
	}
	if h.cache == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "cache is disabled"})
		return
	}
	n, err := h.cache.Clear()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": n})
}

// quotaReset 手动重置某供应商（或 all）的本地用量统计周期。管理员操作。
func (h *Handler) quotaReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}
	var req struct {
		Provider string `json:"provider"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Provider)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provider is required (or \"all\")"})
		return
	}
	if strings.EqualFold(name, "all") {
		if h.store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "telemetry disabled"})
			return
		}
		names, err := h.store.QuotaProviders()
		if err != nil {
			writeError(w, err)
			return
		}
		for _, p := range names {
			if err := h.quota.ResetProvider(p); err != nil {
				writeError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"reset": names})
		return
	}
	if err := h.quota.ResetProvider(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": []string{name}})
}

// quotaAdjust 把某供应商的展示用量修正为指定值（真实调用记录不动）。
// 管理员操作。
func (h *Handler) quotaAdjust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}
	var req struct {
		Provider string  `json:"provider"`
		SetUsed  float64 `json:"set_used"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Provider)
	if name == "" || req.SetUsed < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provider and non-negative set_used are required"})
		return
	}
	if err := h.quota.SetProviderUsed(name, int64(req.SetUsed)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"adjusted": name, "set_used": req.SetUsed})
}

// WarmQuota 启动时异步预热额度缓存，让控制台首次打开就有数据。
// 只读且不产生计费调用；失败静默——首次访问面板时会按需重试。
func (h *Handler) WarmQuota() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = h.quota.Get(ctx)
	}()
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": fmt.Sprintf("%v", err)})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
}

// clients exposes retained search-call usage, not model tokens or account quota.
func (h *Handler) clients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "dashboard telemetry disabled"})
		return
	}
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 1 && parsed != 7 && parsed != 30) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "days must be 1, 7 or 30"})
			return
		}
		days = parsed
	}
	usage, days, err := h.store.AttributionUsageStats(days)
	if err != nil {
		writeError(w, err)
		return
	}
	for i := range usage {
		usage[i].Project = h.projectForClient(usage[i].Client, usage[i].Project)
	}
	rows, err := h.store.ClientUsageStats(days)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"window_days": days, "clients": rows, "usage": usage})
}

// brandLogo 提供品牌 logo 本地文件（dashboard.brand.logo 配置为路径时生效）。
// 访问边界与控制台一致（guard 注入），http(s) URL 不经过本端点。
func (h *Handler) brandLogo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	l := strings.TrimSpace(h.conf.Dashboard.Brand.Logo)
	if l == "" || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, l)
}
