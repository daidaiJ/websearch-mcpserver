package dashboard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	mcpserver "websearch/mcp"
	"websearch/pkg/admission"
	"websearch/pkg/config"
	"websearch/pkg/errcode"
	"websearch/pkg/fetch/everything"
	"websearch/pkg/i18n"
	"websearch/pkg/quota"
	"websearch/pkg/redact"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"
)

var diagnosticGate = admission.NewGate(1, 0)

type diagnosticSourceOption struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Confirm bool   `json:"confirm"`
}

type diagnosticOptions struct {
	Sources       []diagnosticSourceOption `json:"sources"`
	FetchServices []string                 `json:"fetch_services"`
}

func diagnosticOptionsFor(conf config.Config, sources []SourceView) diagnosticOptions {
	out := diagnosticOptions{FetchServices: mcpserver.DiagnosticFetchServices(conf)}
	for _, source := range sources {
		if diagnosticSourceAvailable(source) {
			out.Sources = append(out.Sources, diagnosticSourceOption{
				ID: source.ID, Name: source.Name, Confirm: search.DiagnosticSearchNeedsConfirmation(conf, source.ID),
			})
		}
	}
	return out
}

func diagnosticSourceAvailable(source SourceView) bool {
	if source.Group == "academic" {
		return source.Active && search.IsAcademicDiagnosticSource(source.ID)
	}
	return source.Configured && source.Enabled
}

type diagnosticLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type diagnosticResult struct {
	Fetch       *mcpserver.FetchDiagnostic `json:"fetch,omitempty"`
	OK          bool                       `json:"ok"`
	ElapsedMS   int64                      `json:"elapsed_ms"`
	Endpoint    string                     `json:"endpoint,omitempty"`
	ErrorKind   string                     `json:"error_kind,omitempty"`
	ErrorDetail string                     `json:"error_detail,omitempty"`
	ResultCount *int                       `json:"result_count,omitempty"`
	Titles      []string                   `json:"titles,omitempty"`
	Links       []diagnosticLink           `json:"links,omitempty"`
}

func (h *Handler) registerDiagnostics(mux *http.ServeMux, localAction func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/__admin/api/diagnostics/fetch", localAction(h.diagnoseFetch))
	mux.HandleFunc("/__admin/api/diagnostics/search", localAction(h.diagnoseSearch))
	mux.HandleFunc("/__admin/api/diagnostics/proxy", localAction(h.probeProxy))
	mux.HandleFunc("/__admin/api/diagnostics/everything", localAction(h.probeEverything))
}

func diagnosticPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}

func (h *Handler) probeProxy(w http.ResponseWriter, r *http.Request) {
	if !diagnosticPost(w, r) {
		return
	}
	release, ok := acquireDiagnostic(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	result := diagnosticResult{}
	ep := h.conf.Proxy.GetProxyEndpoint()
	host, display := diagnosticEndpoint(ep)
	result.Endpoint = display
	switch {
	case ep == "":
		result.ErrorKind = "unconfigured"
	case host == "":
		result.ErrorKind = "invalid_endpoint"
	default:
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", host)
		if err == nil {
			conn.Close()
			result.OK = true
		} else {
			result.ErrorKind = diagnosticFailure(ctx, errcode.Wrap(errcode.Network, err))
		}
	}
	result.ElapsedMS = time.Since(start).Milliseconds()
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) probeEverything(w http.ResponseWriter, r *http.Request) {
	if !diagnosticPost(w, r) {
		return
	}
	release, ok := acquireDiagnostic(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	result := diagnosticResult{}
	cfg := h.conf.Everything
	_, result.Endpoint = diagnosticEndpoint(cfg.URL)
	switch {
	case cfg.URL == "":
		result.ErrorKind = "unconfigured"
	case result.Endpoint == "":
		result.ErrorKind = "invalid_endpoint"
	default:
		err := mcpserver.ReprobeEverything(ctx, cfg)
		result.OK = err == nil
		if errors.Is(err, everything.ErrAuth) {
			err = errcode.Wrap(errcode.AccessDenied, err)
		} else {
			err = errcode.Wrap(errcode.Network, err)
		}
		result.ErrorKind = diagnosticFailure(ctx, err)
	}
	result.ElapsedMS = time.Since(start).Milliseconds()
	writeJSON(w, http.StatusOK, result)
}

func diagnosticEndpoint(raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", ""
	}
	port := u.Port()
	switch u.Scheme {
	case "http":
		if port == "" {
			port = "80"
		}
	case "https":
		if port == "" {
			port = "443"
		}
	case "socks5", "socks5h":
		if port == "" {
			port = "1080"
		}
	default:
		return "", ""
	}
	return net.JoinHostPort(u.Hostname(), port), u.Scheme + "://" + u.Host
}

func diagnosticFailure(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return string(errcode.Timeout)
	}
	if errors.Is(err, admission.ErrBusy) {
		return "busy"
	}
	if code := errcode.CodeOf(err); code != "" {
		return string(code)
	}
	return telemetry.ClassifyErrorKind(err)
}

func (h *Handler) diagnoseSearch(w http.ResponseWriter, r *http.Request) {
	if !diagnosticPost(w, r) {
		return
	}
	var req struct {
		Source  string `json:"source"`
		Query   string `json:"query"`
		Confirm bool   `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := decodeJSON(r, &req); err != nil {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.invalid_input"))
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" || utf8.RuneCountInString(req.Query) > 300 {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.invalid_input"))
		return
	}
	var chosen SourceView
	for _, source := range SourceCatalog(h.conf, i18n.FromContext(r.Context())) {
		if source.ID == req.Source && diagnosticSourceAvailable(source) {
			chosen = source
			break
		}
	}
	if chosen.ID == "" {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.source_unavailable"))
		return
	}
	if search.DiagnosticSearchNeedsConfirmation(h.conf, chosen.ID) && !req.Confirm {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.confirm_required"))
		return
	}
	release, ok := acquireDiagnostic(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(admission.WithDiagnostic(r.Context()), 10*time.Second)
	defer cancel()
	start := time.Now()
	rows, err := search.DiagnosticSearch(ctx, h.conf, h.quota, chosen.ID, req.Query)
	count := len(rows)
	result := diagnosticResult{
		OK:          err == nil,
		ElapsedMS:   time.Since(start).Milliseconds(),
		ErrorKind:   diagnosticFailure(ctx, err),
		ResultCount: &count,
	}
	var quotaSkip *quota.SkipError
	if errors.As(err, &quotaSkip) {
		result.ErrorDetail = quota.SearchSkipMessage(i18n.FromContext(r.Context()), quotaSkip.Reason)
	}
	for _, row := range rows[:min(count, 3)] {
		title := redact.Credentials(strings.Join(strings.Fields(row.Title), " "))
		runes := []rune(title)
		if len(runes) > 120 {
			title = string(runes[:120]) + "…"
		}
		if chosen.ID == "pubmed" {
			result.Links = append(result.Links, diagnosticLink{Title: title, URL: row.Url})
		} else {
			result.Titles = append(result.Titles, title)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func diagnosticBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": message})
}

func (h *Handler) diagnoseFetch(w http.ResponseWriter, r *http.Request) {
	if !diagnosticPost(w, r) {
		return
	}
	var req struct {
		URL     string `json:"url"`
		Confirm bool   `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := decodeJSON(r, &req); err != nil {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.invalid_input"))
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" || len(req.URL) > 2048 {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.invalid_input"))
		return
	}
	if !h.conf.CleanFetch.Enabled {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.fetch_disabled"))
		return
	}
	if len(mcpserver.DiagnosticFetchServices(h.conf)) > 0 && !req.Confirm {
		diagnosticBadRequest(w, i18n.TCtx(r.Context(), "dashboard.diagnostics.confirm_required"))
		return
	}
	release, ok := acquireDiagnostic(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	start := time.Now()
	fetch, err := mcpserver.DiagnoseFetchURL(ctx, h.conf, req.URL)
	writeJSON(w, http.StatusOK, diagnosticResult{
		OK: err == nil, ElapsedMS: time.Since(start).Milliseconds(), ErrorKind: diagnosticFailure(ctx, err), Fetch: &fetch,
	})
}

func acquireDiagnostic(w http.ResponseWriter, r *http.Request) (func(), bool) {
	release, err := diagnosticGate.Acquire(r.Context())
	if err != nil {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.diagnostics.busy")})
		return nil, false
	}
	return release, true
}
