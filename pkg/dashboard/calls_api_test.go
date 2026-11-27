package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"websearch/pkg/telemetry"
)

func TestCallsDefaultLimitAndExplicitCompatibility(t *testing.T) {
	h, mux := newTestHandler(t)
	for i := 0; i < 120; i++ {
		if err := h.store.Record(telemetry.Event{Kind: "tool", Tool: "probe", Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/__admin/api/calls", 100},
		{"/__admin/api/calls?limit=20", 20},
		{"/__admin/api/calls?limit=50", 50},
		{"/__admin/api/calls?limit=100", 100},
		{"/__admin/api/calls?limit=invalid", 50},
		{"/__admin/api/events", 50},
	} {
		var rows []telemetry.StoredEvent
		if err := json.Unmarshal(getJSON(t, mux, tc.path), &rows); err != nil || len(rows) != tc.want {
			t.Fatalf("%s: %d rows, want %d, err=%v", tc.path, len(rows), tc.want, err)
		}
	}
}

func TestCallsCursorUsesTimeAndIDAndPreservesFilters(t *testing.T) {
	h, mux := newTestHandler(t)
	const rawQuery = "find https://example.invalid/private/full-query-path?id=secret"
	for i := 0; i < 27; i++ {
		event := telemetry.Event{Kind: "tool", Tool: "smartsearch", RequestID: fmt.Sprintf("cursor-%d", i), Success: true, Query: rawQuery}
		if i%6 == 0 {
			event.Tool = "academicsearch"
		}
		if err := h.store.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string, count int) []telemetry.StoredEvent {
		body := getJSON(t, mux, path)
		var rows []telemetry.StoredEvent
		if err := json.Unmarshal(body, &rows); err != nil || len(rows) != count {
			t.Fatalf("%s: %d rows, want %d, err=%v", path, len(rows), count, err)
		}
		if bytes.Contains(body, []byte(rawQuery)) {
			t.Fatal("raw query in paginated response")
		}
		return rows
	}
	const path = "/__admin/api/calls?tool=smartsearch&status=success"
	full := read(path, 22)
	page := read(path+"&limit=20", 20)
	last := page[len(page)-1]
	occurredAt, err := time.Parse(time.RFC3339, last.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	second := read(fmt.Sprintf("%s&limit=20&before=%d:%d", path, occurredAt.Unix(), last.ID), 2)
	for i, row := range append(page, second...) {
		if row.ID != full[i].ID {
			t.Fatalf("cursor gap or duplicate at offset %d: %d != %d", i, row.ID, full[i].ID)
		}
	}
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, path+"&before=broken", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor: status %d", bad.Code)
	}
}
