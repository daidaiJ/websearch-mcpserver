package telemetry

import (
	"path/filepath"
	"testing"
)

func TestRequestIDRoundTripAndCollector(t *testing.T) {
	ctx := WithRequestID(t.Context(), "req-test-1")
	if got := RequestID(ctx); got != "req-test-1" {
		t.Fatalf("RequestID = %q, want req-test-1", got)
	}
	collector := &RequestCollector{}
	ctx = WithRequestCollector(ctx, collector)
	RecordEventContext(ctx, Event{Kind: "provider", Provider: "demo", Success: true})
	if collector.EventCount() != 1 {
		t.Fatalf("collector buffered %d events, want 1", collector.EventCount())
	}
	events := collector.EventSnapshot()
	if events[0].RequestID != "req-test-1" {
		t.Fatalf("collected request id = %q, want req-test-1", events[0].RequestID)
	}
}

func TestEventFilterMatchesRequestID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := WithRequestID(t.Context(), "req-filter")
	store.RecordContext(ctx, Event{Kind: "tool", Tool: "smartsearch", Query: "q", Success: true})
	store.RecordContext(ctx, Event{Kind: "provider", Provider: "bing", Query: "q", Success: true})
	if err := store.Record(Event{Kind: "provider", Provider: "other", Query: "q", Success: true, RequestID: "req-other"}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.RecentFiltered(EventFilter{RequestID: "req-filter", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("request filter returned %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row.RequestID != "req-filter" {
			t.Fatalf("row request id = %q", row.RequestID)
		}
	}
}