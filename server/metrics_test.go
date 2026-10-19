package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"websearch/pkg/telemetry"
)

func newMetricsTestMux(t *testing.T) (*http.ServeMux, *telemetry.Store) {
	t.Helper()
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "dashboard.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Record(telemetry.Event{Kind: "tool", Tool: "smartsearch", Success: true, Duration: 120 * time.Millisecond, CacheHit: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(telemetry.Event{Kind: "provider", Provider: "bing", Success: false, Duration: 40 * time.Millisecond, Error: errTest("boom")}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{}).registerMetricsHandler(mux, store)
	return mux, store
}

type errTest string

func (e errTest) Error() string { return string(e) }

// TestMetricsEndpointRendersPrometheusText /metrics 输出标准文本格式且含工具/供应商聚合。
func TestMetricsEndpointRendersPrometheusText(t *testing.T) {
	mux, _ := newMetricsTestMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`websearch_tool_requests_total{tool="smartsearch"} 1`,
		`websearch_tool_cache_hit_total{tool="smartsearch"} 1`,
		`websearch_provider_failure_total{provider="bing"} 1`,
		`websearch_provider_duration_ms_bucket{provider="bing",le="50"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("输出缺少 %q\n%s", want, body)
		}
	}
}

// TestMetricsEndpointDisabledWithoutTelemetry 遥测未启用时返回 503，不泄露数据。
func TestMetricsEndpointDisabledWithoutTelemetry(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerMetricsHandler(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 %d, 期望 503", rec.Code)
	}
}

// TestMetricsEndpointRejectsWrites 只读端点拒绝非 GET/HEAD。
func TestMetricsEndpointRejectsWrites(t *testing.T) {
	mux, _ := newMetricsTestMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 %d, 期望 405", rec.Code)
	}
}
