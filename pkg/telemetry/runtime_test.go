package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeSnapshotAggregatesInMemory(t *testing.T) {
	ResetRuntimeMetrics()
	recordRuntime(Event{Kind: "tool", Tool: "smartsearch", Success: true, Duration: 120 * time.Millisecond, CacheHit: true})
	recordRuntime(Event{Kind: "provider", Provider: "bing", Success: false, Duration: 40 * time.Millisecond})
	recordRuntime(Event{Kind: "guard", Tool: "smartsearch", Success: true})

	snapshot := RuntimeSnapshot()
	if len(snapshot.Tools) != 1 || len(snapshot.Sources) != 1 {
		t.Fatalf("unexpected points: %+v", snapshot)
	}
	tool := snapshot.Tools[0]
	if tool.Requests != 1 || tool.Successes != 1 || tool.CacheHits != 1 || tool.Failures != 0 {
		t.Fatalf("tool point = %+v", tool)
	}
	if tool.Buckets[2] != 1 || tool.Buckets[1] != 0 {
		t.Fatalf("tool buckets = %v", tool.Buckets)
	}
	provider := snapshot.Sources[0]
	if provider.Requests != 1 || provider.Failures != 1 || provider.Successes != 0 {
		t.Fatalf("provider point = %+v", provider)
	}
	if len(snapshot.Buckets) != len(RuntimeLatencyBuckets) {
		t.Fatalf("buckets = %v", snapshot.Buckets)
	}

	ResetRuntimeMetrics()
	if got := RuntimeSnapshot(); len(got.Tools) != 0 || len(got.Sources) != 0 {
		t.Fatalf("reset left points: %+v", got)
	}
}

// TestCollectorFlushCountsRuntimeOnce 防止 collector 缓冲与 Flush 落库各记一次，
// 把同一条 tool/provider 事件重复计入 /metrics。
func TestCollectorFlushCountsRuntimeOnce(t *testing.T) {
	ResetRuntimeMetrics()
	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	collector := &RequestCollector{}
	ctx := WithRequestCollector(context.Background(), collector)
	store.RecordContext(ctx, Event{Kind: "tool", Tool: "smartsearch", Success: true})
	if got := RuntimeSnapshot(); len(got.Tools) != 0 {
		t.Fatalf("runtime counted before flush: %+v", got.Tools)
	}
	if n := collector.Flush(store); n != 1 {
		t.Fatalf("flushed %d events, want 1", n)
	}
	snapshot := RuntimeSnapshot()
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Requests != 1 {
		t.Fatalf("runtime points after flush: %+v", snapshot.Tools)
	}
}
