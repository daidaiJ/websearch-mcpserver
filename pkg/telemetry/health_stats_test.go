package telemetry

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthDerivesErrorKindsAndP95(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Six observations: two rate limits, one parse failure, three successes
	// with increasing latency.
	durations := []int64{100, 200, 300, 400, 500, 600}
	for i, d := range durations {
		event := Event{Kind: "provider", Provider: "demo", Query: "q", Duration: time.Duration(d) * time.Millisecond, Success: true}
		switch i {
		case 0, 1:
			event.Success = false
			event.Error = errors.New("HTTP 429 too many requests")
		case 2:
			event.Success = false
			event.Error = errors.New("invalid character '<' looking for beginning of value")
		}
		if err := store.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	health, err := store.health("provider", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(health) != 1 {
		t.Fatalf("health entries = %d, want 1", len(health))
	}
	h := health[0]
	if h.Confidence != "ok" {
		t.Fatalf("confidence = %q, want ok (sample=%d)", h.Confidence, h.SampleSize)
	}
	if h.P95MS != 600 {
		t.Fatalf("p95 = %d, want 600", h.P95MS)
	}
	counts := map[string]int{}
	for _, c := range h.ErrorKinds {
		counts[c.Kind] = c.Count
	}
	if counts[ErrorKindRateLimit] != 2 || counts[ErrorKindParse] != 1 {
		t.Fatalf("error kinds = %+v, want rate_limit=2 parse=1", h.ErrorKinds)
	}
	if h.Status == "down" {
		t.Fatalf("2 failures out of 6 should not be down: %+v", h)
	}
}

func TestHealthMarksSparseSamplesAsLowConfidence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Record(Event{Kind: "provider", Provider: "sparse", Success: false, Error: errors.New("HTTP 429")}); err != nil {
		t.Fatal(err)
	}
	health, err := store.health("provider", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health[0].Confidence != "insufficient" {
		t.Fatalf("confidence = %q, want insufficient", health[0].Confidence)
	}
	if health[0].Status == "down" {
		t.Fatalf("single failure must not be down: %+v", health[0])
	}
}
