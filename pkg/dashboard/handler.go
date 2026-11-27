package dashboard

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/history"
	"websearch/pkg/i18n"
	"websearch/pkg/quota"
	"websearch/pkg/telemetry"
)

// secureEquals 常量时间字符串比较；供管理员用户名/口令校验共用。
func secureEquals(input, want string) bool {
	return input != "" && subtle.ConstantTimeCompare([]byte(input), []byte(want)) == 1
}

const defaultEventLimit = 50

type Handler struct {
	conf             config.Config
	credentialEditor *config.CredentialEditor // protected by credentialsMu
	store            *telemetry.Store
	quota            *quota.Service
	cache            *cache.Cache
	restart          func()
	history          *history.Store
}

// SetSearchHistory attaches the active history store before Register.
func (h *Handler) SetSearchHistory(store *history.Store) {
	h.history = store
}

func New(conf config.Config, store *telemetry.Store, searchCache *cache.Cache, restart func()) *Handler {
	return NewWithQuota(conf, store, searchCache, restart, quota.New(conf, store))
}

func NewWithQuota(conf config.Config, store *telemetry.Store, searchCache *cache.Cache, restart func(), service *quota.Service) *Handler {
	return &Handler{conf: conf, store: store, quota: service, cache: searchCache, restart: restart}
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

// authorizeLocalAction permits listed maintenance operations from the actual
// loopback/write-allowlisted peer and rejects cross-origin browser mutations.
func (h *Handler) authorizeLocalAction(w http.ResponseWriter, r *http.Request) bool {
	if allowed, reason := h.localActionAccess(r); !allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": reason})
		return false
	}
	if err := localActionOrigins.Check(r); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.write.cross_origin")})
		return false
	}
	return true
}

var localActionOrigins = http.NewCrossOriginProtection()

// authorizeWrite additionally authenticates configuration, credentials, service
// restart, private scope and quota ledger requests.
func (h *Handler) authorizeWrite(w http.ResponseWriter, r *http.Request) bool {
	if allowed, reason := h.localActionAccess(r); !allowed {
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
	h.registerRoutes(mux, guard, guard)
}

// RegisterWithReadGuard applies the server's read network policy to the dashboard.
// The listed runtime POSTs use their own write-network policy instead.
func (h *Handler) RegisterWithReadGuard(mux *http.ServeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	h.registerRoutes(mux, guard, func(next http.HandlerFunc) http.HandlerFunc { return next })
}

func (h *Handler) registerRoutes(mux *http.ServeMux, guard, runtimeGuard func(http.HandlerFunc) http.HandlerFunc) {
	localAction := func(next http.HandlerFunc) http.HandlerFunc {
		return runtimeGuard(i18n.HTTP(func(w http.ResponseWriter, r *http.Request) {
			if h.authorizeLocalAction(w, r) {
				next(w, r)
			}
		}))
	}
	h.registerDashboardAssets(mux, guard)
	mux.HandleFunc("/__admin/api/overview", guard(i18n.HTTP(h.overview)))
	mux.HandleFunc("/__admin/api/events", guard(i18n.HTTP(h.events)))
	mux.HandleFunc("/__admin/api/calls", guard(i18n.HTTP(h.calls)))
	mux.HandleFunc("/__admin/api/request-events", guard(i18n.HTTP(h.requestEvents)))
	mux.HandleFunc("/__admin/api/quotas", guard(i18n.HTTP(h.quotas)))
	mux.HandleFunc("/__admin/api/quotas/refresh", localAction(h.refreshQuotas))
	mux.HandleFunc("/__admin/api/sources/cooldowns/release", localAction(h.releaseCooldown))
	mux.HandleFunc("/__admin/api/quotas/reset", guard(i18n.HTTP(h.quotaReset)))
	mux.HandleFunc("/__admin/api/quotas/adjust", guard(i18n.HTTP(h.quotaAdjust)))
	mux.HandleFunc("/__admin/api/brand/logo", guard(i18n.HTTP(h.brandLogo)))
	mux.HandleFunc("/__admin/api/clients", guard(i18n.HTTP(h.clients)))
	mux.HandleFunc("/__admin/api/providers", guard(i18n.HTTP(h.providers)))
	mux.HandleFunc("/__admin/api/metrics", guard(i18n.HTTP(h.metricsHandler)))
	mux.HandleFunc("/__admin/api/everything/scope", guard(i18n.HTTP(h.everythingScope)))
	h.registerDiagnostics(mux, localAction)
	mux.HandleFunc("/__admin/api/settings", guard(i18n.HTTP(h.settings)))
	mux.HandleFunc("/__admin/api/secrets", guard(i18n.HTTP(h.secrets)))
	mux.HandleFunc("/__admin/api/restart", guard(i18n.HTTP(h.restartService)))
	mux.HandleFunc("/__admin/api/cache/clear", localAction(h.clearCache))
	mux.HandleFunc("/__admin/api/history/clear", localAction(h.clearHistory))
	mux.HandleFunc("/__admin/api/fetchdata/clear", localAction(h.clearTemporaryOutputs))
	mux.HandleFunc("/__admin/api/storage", guard(i18n.HTTP(h.storageStats)))
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
	writeJSON(w, http.StatusOK, h.dashboardOverview(r.Context(), observed))
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
	filter := telemetry.CallFilter{
		Tool: strings.TrimSpace(query.Get("tool")), Provider: strings.TrimSpace(query.Get("provider")),
		Status: eventStatus(query.Get("status")), ErrorKind: eventErrorKind(query.Get("error_kind")),
		Limit: eventLimit(limit),
	}
	if cursor := query.Get("before"); cursor != "" {
		rawAt, rawID, ok := strings.Cut(cursor, ":")
		at, atErr := strconv.ParseInt(rawAt, 10, 64)
		id, idErr := strconv.ParseInt(rawID, 10, 64)
		if !ok || atErr != nil || idErr != nil || at <= 0 || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.traces.invalid_cursor")})
			return
		}
		filter.BeforeAt, filter.BeforeID = at, id
	}
	switch query.Get("cache_hit") {
	case "true", "false":
		hit := query.Get("cache_hit") == "true"
		filter.CacheHit = &hit
	}
	filter.Application = strings.TrimSpace(query.Get("application"))
	filter.Project = strings.TrimSpace(query.Get("project"))
	filter.ProjectForClient = func(client string) string { return h.projectForClient(client, "") }
	out, err := h.store.RecentCallsContext(r.Context(), filter)
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
	loc := i18n.FromContext(r.Context())
	for i := range out {
		if out[i].Kind == telemetry.QuotaSkipEvent {
			out[i].SkipReason = quota.SearchSkipMessage(loc, out[i].Detail)
		}
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
	view := h.dashboardOverview(r.Context(), observed)
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

// everythingScope discloses local paths only to an authorized administrator.
func (h *Handler) everythingScope(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}
	endpoint := ""
	if u, err := url.Parse(h.conf.Everything.URL); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		endpoint = u.Scheme + "://" + u.Host
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": endpoint, "roots": append([]string{}, h.conf.Everything.Roots...)})
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	overlay := h.conf.SettingsFilePaths().Overlay
	if overlay == "" {
		overlay = config.DashboardOverlayPath(&h.conf)
	}
	loc := i18n.FromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		out := settingsFileView(h.conf, config.GetConfigFile(), overlay, loc)
		credentialStatus, err := h.credentialView()
		out["credential_status"] = credentialStatus
		if err != nil {
			out["credential_error"] = loc.T("dashboard.credentials.file_unavailable")
		}
		allowed, reason := h.localActionAccess(r)
		out["local_action_allowed"] = allowed
		if allowed && !h.conf.Dashboard.AdminPasswordConfigured() {
			allowed, reason = false, loc.T("dashboard.write.no_password")
		}
		if failure, failed := out["config_error"].(string); failed {
			allowed, reason = false, failure
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
		out, err := applySettingsFile(h.conf, req, config.GetConfigFile(), overlay, func() error {
			if h.cache == nil {
				return nil
			}
			_, err := h.cache.Clear()
			return err
		}, loc)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errSettingsConflict) || errors.Is(err, config.ErrSettingsPathsChanged) {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"error": settingsErrorMessage(loc, err)})
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) localActionAccess(r *http.Request) (bool, string) {
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
