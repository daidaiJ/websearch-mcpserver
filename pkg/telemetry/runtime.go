package telemetry

import (
	"sort"
	"sync"
	"time"
)

// RuntimeLatencyBuckets 是进程内业务指标的延迟分桶。它与 dashboard 的
// 默认分桶保持一致，避免 /metrics 抓取依赖 SQLite 聚合。
var RuntimeLatencyBuckets = []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000}

type runtimeRegistry struct {
	mu      sync.Mutex
	day     string
	tools   map[string]*MetricsPoint
	sources map[string]*MetricsPoint
}

var runtimeMetrics = newRuntimeRegistry()

func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{
		day:     time.Now().Format("2006-01-02"),
		tools:   make(map[string]*MetricsPoint),
		sources: make(map[string]*MetricsPoint),
	}
}

// recordRuntime 由所有事件入口调用一次；只保留 tool/provider 的计数与延迟，
// 不保存查询、URL 或错误原文。跨天时清空，内存上限随来源数量增长。
func recordRuntime(e Event) {
	kind := e.Kind
	if kind == "" {
		kind = "tool"
	}
	if kind != "tool" && kind != "provider" {
		return
	}
	name := e.Tool
	if kind == "provider" {
		name = e.Provider
	}
	if name == "" {
		return
	}
	runtimeMetrics.record(kind, name, e)
}

func (r *runtimeRegistry) record(kind, name string, e Event) {
	now := time.Now()
	day := now.Format("2006-01-02")
	durationMS := e.Duration.Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.day != day {
		r.day = day
		r.tools = make(map[string]*MetricsPoint)
		r.sources = make(map[string]*MetricsPoint)
	}
	target := r.tools
	if kind == "provider" {
		target = r.sources
	}
	point, ok := target[name]
	if !ok {
		point = &MetricsPoint{Kind: kind, Name: name, Buckets: make([]int64, len(RuntimeLatencyBuckets))}
		target[name] = point
	}
	point.Requests++
	if e.Success {
		point.Successes++
	} else {
		point.Failures++
	}
	if e.CacheHit {
		point.CacheHits++
	}
	point.DurationMS += durationMS
	point.DurationCount++
	for i, bound := range RuntimeLatencyBuckets {
		if float64(durationMS) <= bound {
			point.Buckets[i]++
		}
	}
}

// RuntimeSnapshot 返回当前进程的 tool/provider 聚合；读路径不触发数据库查询。
func RuntimeSnapshot() MetricsSnapshot {
	now := time.Now()
	day := now.Format("2006-01-02")
	runtimeMetrics.mu.Lock()
	defer runtimeMetrics.mu.Unlock()
	if runtimeMetrics.day != day {
		runtimeMetrics.day = day
		runtimeMetrics.tools = make(map[string]*MetricsPoint)
		runtimeMetrics.sources = make(map[string]*MetricsPoint)
	}
	buckets := append([]float64(nil), RuntimeLatencyBuckets...)
	return MetricsSnapshot{
		Day:     day,
		Buckets: buckets,
		Tools:   copyMetricsPoints(runtimeMetrics.tools),
		Sources: copyMetricsPoints(runtimeMetrics.sources),
	}
}

// ResetRuntimeMetrics 清空进程内聚合，供测试与显式进程重置使用。
func ResetRuntimeMetrics() {
	runtimeMetrics.mu.Lock()
	runtimeMetrics.day = time.Now().Format("2006-01-02")
	runtimeMetrics.tools = make(map[string]*MetricsPoint)
	runtimeMetrics.sources = make(map[string]*MetricsPoint)
	runtimeMetrics.mu.Unlock()
}

func copyMetricsPoints(values map[string]*MetricsPoint) []MetricsPoint {
	out := make([]MetricsPoint, 0, len(values))
	for _, point := range values {
		copyPoint := *point
		copyPoint.Buckets = append([]int64(nil), point.Buckets...)
		out = append(out, copyPoint)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
