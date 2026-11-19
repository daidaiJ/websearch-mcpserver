package server

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"websearch/pkg/telemetry"
)

// latencyBucketsMS 与 dashboard 端点共用同一套延迟分桶（毫秒）。
var latencyBucketsMS = telemetry.RuntimeLatencyBuckets

// registerMetricsHandler 暴露标准 Prometheus 文本端点 /metrics。
//
// 进程内指标（在飞请求、uptime、goroutine、堆分配）始终可用；工具/供应商
// 聚合来自进程内计数器，不依赖遥测库或 SQLite。遥测库只负责 dashboard 的
// 持久化，抓取 /metrics 时不再回读数据库。
func (s *Server) registerMetricsHandler(mux *http.ServeMux, metrics *telemetry.Store) {
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var sb strings.Builder
		renderProcessMetrics(&sb, s, metrics != nil)
		sb.WriteString(renderMetricsText(telemetry.RuntimeSnapshot()))
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(sb.String()))
	})
}

// renderProcessMetrics 输出不依赖遥测库的进程内指标。
func renderProcessMetrics(sb *strings.Builder, s *Server, telemetryEnabled bool) {
	var inflight int64
	uptime := 0.0
	if s != nil {
		inflight = s.inflight.Load()
		if !s.startedAt.IsZero() {
			uptime = time.Since(s.startedAt).Seconds()
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	fmt.Fprintf(sb, "# HELP websearch_process_inflight_requests HTTP requests currently being served\n")
	fmt.Fprintf(sb, "# TYPE websearch_process_inflight_requests gauge\n")
	fmt.Fprintf(sb, "websearch_process_inflight_requests %d\n", inflight)
	fmt.Fprintf(sb, "# HELP websearch_process_uptime_seconds Seconds since the process started\n")
	fmt.Fprintf(sb, "# TYPE websearch_process_uptime_seconds gauge\n")
	fmt.Fprintf(sb, "websearch_process_uptime_seconds %.3f\n", uptime)
	fmt.Fprintf(sb, "# HELP websearch_process_goroutines Number of goroutines\n")
	fmt.Fprintf(sb, "# TYPE websearch_process_goroutines gauge\n")
	fmt.Fprintf(sb, "websearch_process_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(sb, "# HELP websearch_process_heap_alloc_bytes Bytes of allocated heap objects\n")
	fmt.Fprintf(sb, "# TYPE websearch_process_heap_alloc_bytes gauge\n")
	fmt.Fprintf(sb, "websearch_process_heap_alloc_bytes %d\n", mem.HeapAlloc)
	fmt.Fprintf(sb, "# HELP websearch_telemetry_enabled Whether persistent telemetry series are enabled\n")
	fmt.Fprintf(sb, "# TYPE websearch_telemetry_enabled gauge\n")
	if telemetryEnabled {
		sb.WriteString("websearch_telemetry_enabled 1\n")
	} else {
		sb.WriteString("websearch_telemetry_enabled 0\n")
	}
}

// renderMetricsText 渲染 OpenMetrics/Prometheus 文本格式。
func renderMetricsText(snapshot telemetry.MetricsSnapshot) string {
	var sb strings.Builder
	buckets := snapshot.Buckets
	if len(buckets) == 0 {
		buckets = latencyBucketsMS
	}
	if len(snapshot.Tools) > 0 {
		sb.WriteString("# HELP websearch_tool_requests_total MCP tool calls recorded by this process (in-memory)\n")
		sb.WriteString("# TYPE websearch_tool_requests_total counter\n")
		for _, point := range snapshot.Tools {
			labels := fmt.Sprintf(`tool="%s"`, escapeMetricsLabel(point.Name))
			writeMetricsPoint(&sb, "websearch_tool", labels, point, buckets)
		}
	}
	if len(snapshot.Sources) > 0 {
		sb.WriteString("# HELP websearch_provider_requests_total Search provider calls recorded by this process (in-memory)\n")
		sb.WriteString("# TYPE websearch_provider_requests_total counter\n")
		for _, point := range snapshot.Sources {
			labels := fmt.Sprintf(`provider="%s"`, escapeMetricsLabel(point.Name))
			writeMetricsPoint(&sb, "websearch_provider", labels, point, buckets)
		}
	}
	return sb.String()
}

func writeMetricsPoint(sb *strings.Builder, prefix, labels string, point telemetry.MetricsPoint, buckets []float64) {
	total := point.Requests
	fmt.Fprintf(sb, "%s_requests_total{%s} %d\n", prefix, labels, total)
	fmt.Fprintf(sb, "%s_success_total{%s} %d\n", prefix, labels, point.Successes)
	fmt.Fprintf(sb, "%s_failure_total{%s} %d\n", prefix, labels, point.Failures)
	fmt.Fprintf(sb, "%s_cache_hit_total{%s} %d\n", prefix, labels, point.CacheHits)
	if point.DurationCount > 0 {
		fmt.Fprintf(sb, "%s_duration_ms_sum{%s} %d\n", prefix, labels, point.DurationMS)
		fmt.Fprintf(sb, "%s_duration_ms_count{%s} %d\n", prefix, labels, point.DurationCount)
	}
	fmt.Fprintf(sb, "%s_duration_ms_bucket{%s,le=\"+Inf\"} %d\n", prefix, labels, total)
	for i, bound := range buckets {
		if i >= len(point.Buckets) {
			break
		}
		fmt.Fprintf(sb, "%s_duration_ms_bucket{%s,le=\"%g\"} %d\n", prefix, labels, bound, point.Buckets[i])
	}
}

func escapeMetricsLabel(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n")
	return replacer.Replace(value)
}
