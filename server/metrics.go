package server

import (
	"fmt"
	"net/http"
	"strings"

	"websearch/pkg/telemetry"
)

// latencyBucketsMS 与 dashboard 端点共用同一套延迟分桶（毫秒）。
var latencyBucketsMS = []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000}

// registerMetricsHandler 暴露标准 Prometheus 文本端点 /metrics。
//
// 只读脱敏聚合：数据来自遥测库（查询仅以哈希/关键词落库），不含密钥、URL
// 或查询原文；不做主动探测。该端点与 loopback 写守卫无关（写端点仍受
// dashboard 访问控制约束），遥测未启用时返回 503。
func (s *Server) registerMetricsHandler(mux *http.ServeMux, metrics *telemetry.Store) {
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if metrics == nil {
			http.Error(w, "telemetry disabled (dashboard.enabled=false)", http.StatusServiceUnavailable)
			return
		}
		snapshot, err := metrics.Metrics(latencyBucketsMS)
		if err != nil {
			http.Error(w, "metrics unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(renderMetricsText(snapshot)))
	})
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
