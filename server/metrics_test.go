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
	telemetry.ResetRuntimeMetrics()
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
	New().registerMetricsHandler(mux, store)
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
		`websearch_process_inflight_requests 0`,
		`websearch_process_uptime_seconds `,
		`websearch_process_goroutines `,
		`websearch_telemetry_enabled 1`,
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

// TestMetricsEndpointWithoutTelemetry 遥测未启用时仍输出进程内指标与
// 进程内业务计数；只有持久化序列被关闭。
func TestMetricsEndpointWithoutTelemetry(t *testing.T) {
	telemetry.ResetRuntimeMetrics()
	mux := http.NewServeMux()
	New().registerMetricsHandler(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`websearch_process_inflight_requests `,
		`websearch_process_uptime_seconds `,
		`websearch_telemetry_enabled 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("输出缺少 %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "websearch_tool_requests_total") {
		t.Errorf("没有事件时不应输出工具聚合\n%s", body)
	}
}

// TestMetricsEndpointWithoutTelemetryUsesRuntimeCounters 验证 dashboard
// 关闭时业务计数仍由进程内聚合提供，不依赖 SQLite。
func TestMetricsEndpointWithoutTelemetryUsesRuntimeCounters(t *testing.T) {
	telemetry.ResetRuntimeMetrics()
	old := telemetry.Default()
	telemetry.SetDefault(nil)
	t.Cleanup(func() { telemetry.SetDefault(old) })
	telemetry.Record(telemetry.Event{Kind: "tool", Tool: "smartsearch", Success: true, Duration: 120 * time.Millisecond})

	mux := http.NewServeMux()
	New().registerMetricsHandler(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`websearch_telemetry_enabled 0`,
		`websearch_tool_requests_total{tool="smartsearch"} 1`,
		`websearch_tool_duration_ms_bucket{tool="smartsearch",le="250"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("输出缺少 %q\n%s", want, body)
		}
	}
}

// TestInstrumentTracksInflight instrument 在请求处理期间递增、结束后归零。
func TestInstrumentTracksInflight(t *testing.T) {
	s := New()
	started := make(chan struct{})
	release := make(chan struct{})
	h := s.instrument(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	<-started
	if got := s.inflight.Load(); got != 1 {
		t.Fatalf("inflight = %d, want 1", got)
	}
	close(release)
	<-done
	if got := s.inflight.Load(); got != 0 {
		t.Fatalf("inflight after response = %d, want 0", got)
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
