package telemetry

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRecordsToolDetail(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Record(Event{
		Kind:        "tool",
		Tool:        "pdf_parser",
		Success:     true,
		Duration:    time.Millisecond,
		ResultCount: 3,
		Detail:      "mineru-remote",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := store.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d events, want 1", len(rows))
	}
	if rows[0].Detail != "mineru-remote" {
		t.Fatalf("detail = %q, want %q", rows[0].Detail, "mineru-remote")
	}
	if rows[0].ResultCount != 3 {
		t.Fatalf("result_count = %d, want 3", rows[0].ResultCount)
	}
}
