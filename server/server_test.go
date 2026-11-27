package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

// newAdminMux 注册 admin 路由用于测试观测。
func newAdminMux(initRef int32) (*Server, *http.ServeMux) {
	s := New()
	s.SetRefCount(initRef)
	mux := http.NewServeMux()
	s.registerAdminHandlers(mux, *config.Default(), nil)
	return s, mux
}

// doReq 发起请求，remoteAddr 为空则使用本地地址。
func doReq(mux *http.ServeMux, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if remoteAddr != "" {
		r.RemoteAddr = remoteAddr
	} else {
		r.RemoteAddr = "127.0.0.1:12345"
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestAdminHealth(t *testing.T) {
	_, mux := newAdminMux(3)
	// health 应远程可访问
	w := doReq(mux, http.MethodGet, "/__admin/health", "", "8.8.8.8:5555")
	if w.Code != http.StatusOK {
		t.Fatalf("health 远程应可访问, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	t.Logf("health resp: %s", w.Body.String())
	if resp["message"] != "running" {
		t.Errorf(`health message 应为 "running", got %v`, resp["message"])
	}
	if resp["ref_count"] != float64(3) {
		t.Errorf("health ref_count 应为 3, got %v", resp["ref_count"])
	}
}

func TestAdminLocalOnly(t *testing.T) {
	_, mux := newAdminMux(1)
	// status/refcount/shutdown 远程访问应被拒绝
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/__admin/status", ""},
		{http.MethodPost, "/__admin/refcount", `{"delta":1}`},
		{http.MethodPost, "/__admin/shutdown", ""},
	} {
		w := doReq(mux, tc.method, tc.path, tc.body, "8.8.8.8:5555")
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s 远程应返回 403, got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestAdminStatus(t *testing.T) {
	_, mux := newAdminMux(2)
	w := doReq(mux, http.MethodGet, "/__admin/status", "", "")
	t.Logf("status resp: %s", w.Body.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status 本地应可访问, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ref_count"] != float64(2) {
		t.Errorf("status ref_count 应为 2, got %v", resp["ref_count"])
	}
	// 非 GET 方法应被拒绝
	w = doReq(mux, http.MethodPost, "/__admin/status", "", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status POST 应返回 405, got %d", w.Code)
	}
}

func TestAdminRefCount(t *testing.T) {
	s, mux := newAdminMux(1)
	// +2 → 3
	w := doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":2}`, "")
	t.Logf("refcount +2 resp: %s", w.Body.String())
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ref_count"] != float64(3) {
		t.Errorf("refcount 应为 3, got %v", resp["ref_count"])
	}
	if s.RefCount() != 3 {
		t.Errorf("server refCount 应为 3, got %d", s.RefCount())
	}
	// 归零触发关闭消息
	w = doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":-3}`, "")
	t.Logf("refcount -3 resp: %s", w.Body.String())
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ref_count"] != float64(0) {
		t.Errorf("refcount 应为 0, got %v", resp["ref_count"])
	}
	if resp["message"] == "" || resp["message"] == nil {
		t.Errorf("归零时应返回 shutdown 消息, got %v", resp["message"])
	}
}

func TestEmbeddedHandlerUsesDurableQuotaAndClosesResources(t *testing.T) {
	conf := *config.Default()
	conf.Mode = "apipool"
	conf.Apipool.Engines = []string{"exa"}
	conf.Exa.APIKey = "fixture-key"
	conf.Dashboard.Enabled = true
	conf.Dashboard.AdminPassword = "fixture-admin"
	conf.Dashboard.StoragePath = filepath.Join(t.TempDir(), "dashboard.db")
	conf.Dashboard.Quotas.EnforceProviders = []string{"exa"}
	conf.Dashboard.Quotas.Limits = map[string]int{"exa": 1}
	conf.Dashboard.Quotas.Reset = "none"
	conf.Cache.StoragePath = filepath.Join(t.TempDir(), "cache.db")
	conf.CleanFetch.Enabled, conf.PDFParser.Enabled = false, false

	seed, err := telemetry.Open(conf.Dashboard.StoragePath, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.InitializeSearchQuota("exa", 0, 1); err != nil {
		_ = seed.Close()
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	srv := New()
	handler := srv.Handler(conf)
	t.Cleanup(func() { _ = srv.Close() })
	store := telemetry.Default()
	if store == nil {
		t.Fatal("embedded dashboard has no live telemetry store")
	}
	request := httptest.NewRequest(http.MethodPost, "/__admin/api/diagnostics/search", strings.NewReader(`{"source":"exa","query":"offline quota fixture","confirm":true}`))
	request.RemoteAddr = "127.0.0.1:1"
	request.Header.Set("X-Admin-Password", conf.Dashboard.AdminPassword)
	request.Header.Set("Accept-Language", "en")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Configured search quota reached") {
		t.Fatalf("embedded search did not use the preseeded local hard limit: %d %s", recorder.Code, recorder.Body)
	}

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if telemetry.Default() != nil {
		t.Fatal("closed handler left its telemetry store published")
	}
	if _, err := store.SearchQuotaUsage("exa", 0); err == nil {
		t.Fatal("closed handler left its SQLite store open")
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedHandlerLeavesQuotaStorageClosedByDefault(t *testing.T) {
	conf := *config.Default()
	conf.Dashboard.Enabled = false
	conf.Dashboard.StoragePath = filepath.Join(t.TempDir(), "dashboard.db")
	conf.Cache.StoragePath = filepath.Join(t.TempDir(), "cache.db")
	conf.CleanFetch.Enabled, conf.PDFParser.Enabled = false, false
	srv := New()
	_ = srv.Handler(conf)
	t.Cleanup(func() { _ = srv.Close() })
	if _, err := os.Stat(conf.Dashboard.StoragePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default handler opened quota storage: %v", err)
	}
}

func TestEmbeddedQuotaOnlyOpensLedgerWithoutPublishingTelemetry(t *testing.T) {
	conf := *config.Default()
	conf.Dashboard.Enabled = false
	conf.Dashboard.StoragePath = filepath.Join(t.TempDir(), "dashboard.db")
	conf.Dashboard.Quotas.EnforceProviders = []string{"exa"}
	conf.Cache.StoragePath = filepath.Join(t.TempDir(), "cache.db")
	conf.CleanFetch.Enabled, conf.PDFParser.Enabled = false, false
	srv := New()
	handler := srv.Handler(conf)
	t.Cleanup(func() { _ = srv.Close() })
	if _, err := os.Stat(conf.Dashboard.StoragePath); err != nil {
		t.Fatalf("search enforcement did not open its ledger without the dashboard: %v", err)
	}
	if telemetry.Default() != nil {
		t.Fatal("quota-only handler enabled dashboard telemetry")
	}
	request := httptest.NewRequest(http.MethodGet, "/__admin/api/quotas", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("quota-only handler published dashboard endpoints: %d", recorder.Code)
	}
}

// TestDashboardRuntimeNetworkScope exercises the actual server routing, including
// write-only peers that are outside the dashboard read network.
func TestDashboardRuntimeNetworkScope(t *testing.T) {
	conf := config.Config{}
	endpoint := httptest.NewServer(http.NotFoundHandler())
	defer endpoint.Close()
	conf.Proxy.Enabled, conf.Proxy.Endpoint = true, endpoint.URL
	conf.Dashboard.Enabled = true
	conf.Dashboard.AllowedNetworks = []string{"192.0.2.10"}
	conf.Dashboard.WriteAllowedNetworks = []string{"198.51.100.32/28"}
	mux := http.NewServeMux()
	New().registerAdminHandlers(mux, conf, nil)
	for _, path := range []string{"cache/clear", "history/clear", "fetchdata/clear", "quotas/refresh", "sources/cooldowns/release", "diagnostics/proxy", "diagnostics/everything", "diagnostics/search", "diagnostics/fetch"} {
		t.Run(path, func(t *testing.T) {
			for _, tc := range []struct {
				peer, origin string
				allowed      bool
			}{
				{"127.0.0.1:99", "", true}, {"[::1]:99", "", true}, {"198.51.100.35:99", "", true},
				{"192.0.2.10:99", "", false}, {"203.0.113.1:99", "", false}, {"127.0.0.1:99", "https://other.example", false},
			} {
				req := httptest.NewRequest(http.MethodPost, "http://localhost/__admin/api/"+path, strings.NewReader("{}"))
				req.RemoteAddr = tc.peer
				req.Header.Set("X-Forwarded-For", "127.0.0.1")
				req.Header.Set("X-Real-IP", "198.51.100.35")
				if tc.origin != "" {
					req.Header.Set("Origin", tc.origin)
				}
				res := httptest.NewRecorder()
				mux.ServeHTTP(res, req)
				want := http.StatusForbidden
				if tc.allowed {
					want = http.StatusBadRequest // empty payload reached the action
					if path == "diagnostics/proxy" || path == "diagnostics/everything" {
						want = http.StatusOK // probes accept empty payloads
					}
				}
				if res.Code != want {
					t.Errorf("peer %s origin %q: got %d want %d", tc.peer, tc.origin, res.Code, want)
				}
			}
		})
	}
	for _, path := range []string{"settings", "secrets", "restart", "quotas/reset", "quotas/adjust", "everything/scope"} {
		method := http.MethodPost
		if path == "everything/scope" {
			method = http.MethodGet
		}
		req := httptest.NewRequest(method, "/__admin/api/"+path, strings.NewReader("{}"))
		req.RemoteAddr = "127.0.0.1:99"
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Errorf("protected %s: got %d", path, res.Code)
		}
	}
}
