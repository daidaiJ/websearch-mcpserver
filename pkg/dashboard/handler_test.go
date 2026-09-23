package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

func newTestHandler(t *testing.T) (*Handler, *http.ServeMux) {
	t.Helper()
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := New(config.Config{}, store, nil, nil)
	mux := http.NewServeMux()
	handler.Register(mux, func(fn http.HandlerFunc) http.HandlerFunc { return fn })
	return handler, mux
}

func getJSON(t *testing.T, mux *http.ServeMux, path string) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d body %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func TestOverviewExposesDashboardSources(t *testing.T) {
	handler, mux := newTestHandler(t)
	if err := handler.store.Record(telemetry.Event{Kind: "provider", Provider: "doubao", Query: "hello", Success: true, Duration: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(getJSON(t, mux, "/__admin/api/overview"), &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"generated_at", "today", "providers", "tools", "trend", "system", "sources", "academic_sources"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("overview response missing %q", key)
		}
	}
}

func TestEventsFiltersKindStatusAndSource(t *testing.T) {
	handler, mux := newTestHandler(t)
	seeded := []telemetry.Event{
		{Kind: "provider", Provider: "doubao", Query: "alpha", Success: true, Duration: time.Millisecond},
		{Kind: "provider", Provider: "tavily", Query: "beta", Success: false, Duration: time.Millisecond, Error: errors.New("upstream failed")},
		{Kind: "tool", Tool: "smartsearch", Query: "gamma", Success: true, Duration: time.Millisecond},
		{Kind: "tool", Tool: "cleanfetch", Query: "delta", Success: false, Duration: time.Millisecond, Error: errors.New("fetch failed")},
	}
	for _, event := range seeded {
		if err := handler.store.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		query string
		want  int
	}{
		{"", 4},
		{"kind=provider", 2},
		{"kind=tool", 2},
		{"kind=none", 0},
		{"status=all", 4},
		{"status=failure", 2},
		{"kind=provider&status=failure", 1},
		{"kind=provider&status=failure&source=tavily", 1},
		{"kind=tool&source=cleanfetch", 1},
		{"kind=tool&source=tavily", 0},
		{"kind=bogus&status=bogus", 4},
	}
	for _, tc := range cases {
		var out []telemetry.StoredEvent
		if err := json.Unmarshal(getJSON(t, mux, "/__admin/api/events?"+tc.query), &out); err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		if len(out) != tc.want {
			t.Fatalf("events?%s: got %d events, want %d", tc.query, len(out), tc.want)
		}
	}
}

func TestEventsLimitUsesPageSizesAndFallsBackToFifty(t *testing.T) {
	handler, mux := newTestHandler(t)
	for i := 0; i < 60; i++ {
		if err := handler.store.Record(telemetry.Event{Kind: "tool", Tool: "smartsearch", Query: "q", Success: true, Duration: time.Millisecond}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		query string
		want  int
	}{
		{"limit=20", 20},
		{"limit=50", 50},
		{"limit=100", 60},
		{"limit=25", 50},
		{"limit=0", 50},
		{"limit=-7", 50},
		{"limit=1000", 50},
		{"limit=abc", 50},
		{"", 50},
	}
	for _, tc := range cases {
		var out []telemetry.StoredEvent
		if err := json.Unmarshal(getJSON(t, mux, "/__admin/api/events?"+tc.query), &out); err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		if len(out) != tc.want {
			t.Fatalf("events?%s: got %d events, want %d", tc.query, len(out), tc.want)
		}
	}
}
