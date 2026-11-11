package server

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"websearch/pkg/log"
	"websearch/pkg/telemetry"
)

// latencyBucketsMS 与 dashboard 端点共用同一套延迟分桶（毫秒）。
var latencyBucketsMS = []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000}

// registerMetricsHandler 暴露标准 Prometheus 文本端点 /metrics。
//
// 进程内指标（在飞请求、uptime、goroutine、堆分配）始终可用；工具/供应商
// 聚合来自遥测库，遥测未启用时只省略这部分序列。只读脱敏，不含密钥、URL
// 或查询原文；该端点与 loopback 写守卫无关（写端点仍受 dashboard 访问控制）。
func (s *Server) registerMetricsHandler(mux *http.ServeMux, metrics *telemetry.Store) {
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var sb strings.Builder
		renderProcessMetrics(&sb, s, metrics != nil)
		if metrics != nil {
			snapshot, err := metrics.Metrics(latencyBucketsMS)
			if err != nil {
				log.ErrorT("log.server.metrics_query_failed", "err", err)
			} else {
				sb.WriteString(renderMetricsText(snapshot))
			}
		}
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
	sb.WriteString("# HELP websearch_tool_requests_total MCP tool calls recorded today (passive telemetry)\n")
	sb.WriteString("# TYPE websearch_tool_requests_total counter\n")
	for _, point := range snapshot.Tools {
		labels := fmt.Sprintf(`tool="%s"`, escapeMetricsLabel(point.Name))
		writeMetricsPoint(&sb, "websearch_tool", labels, point)
	}
	sb.WriteString("# HELP websearch_provider_requests_total Search provider calls recorded today (passive telemetry)\n")
	sb.WriteString("# TYPE websearch_provider_requests_total counter\n")
	for _, point := range snapshot.Sources {
		labels := fmt.Sprintf(`provider="%s"`, escapeMetricsLabel(point.Name))
		writeMetricsPoint(&sb, "websearch_provider", labels, point)
	}
	return sb.String()
}

func writeMetricsPoint(sb *strings.Builder, prefix, labels string, point telemetry.MetricsPoint) {
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
	for i, bound := range latencyBucketsMS {
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
