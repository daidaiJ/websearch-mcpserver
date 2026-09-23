package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"websearch/pkg/telemetry"
)

var latencyBucketsMS = []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000}

// metricsHandler exposes the same passive telemetry as OpenMetrics/Prometheus
// text. It only reads persisted events and never performs active probing.
func (h *Handler) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.store == nil {
		http.Error(w, "dashboard telemetry disabled", http.StatusServiceUnavailable)
		return
	}
	snapshot, err := h.store.Metrics(latencyBucketsMS)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(renderMetricsText(snapshot)))
}

func renderMetricsText(snapshot telemetry.MetricsSnapshot) string {
	var sb strings.Builder
	sb.WriteString("# HELP websearch_tool_requests_total MCP tool calls recorded today (passive telemetry)\n")
	sb.WriteString("# TYPE websearch_tool_requests_total counter\n")
	for _, point := range snapshot.Tools {
		labels := fmt.Sprintf(`tool="%s"`, escapeLabel(point.Name))
		writeMetricsPoint(&sb, "websearch_tool", labels, point)
	}
	sb.WriteString("# HELP websearch_provider_requests_total Search provider calls recorded today (passive telemetry)\n")
	sb.WriteString("# TYPE websearch_provider_requests_total counter\n")
	for _, point := range snapshot.Sources {
		labels := fmt.Sprintf(`provider="%s"`, escapeLabel(point.Name))
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

func escapeLabel(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n")
	return replacer.Replace(value)
}
