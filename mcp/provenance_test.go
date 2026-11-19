package mcpserver

import (
	"strings"
	"testing"
	"time"
)

func TestProvenanceHeaderForSession(t *testing.T) {
	provenanceSessions.reset()
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	first := provenanceHeaderForSession("session-a", at, 0)
	if !strings.Contains(first, "usage_note") || !strings.Contains(first, "retrieved_at") {
		t.Fatalf("first call should carry the full header, got %q", first)
	}
	if second := provenanceHeaderForSession("session-a", at.Add(time.Minute), 0); second != "" {
		t.Fatalf("repeated fresh call should omit the header, got %q", second)
	}
	cached := provenanceHeaderForSession("session-a", at, 2*time.Hour)
	if !strings.Contains(cached, "cache_age_seconds: 7200") || strings.Contains(cached, "usage_note") {
		t.Fatalf("cache call should keep only the cache age, got %q", cached)
	}
	if other := provenanceHeaderForSession("session-b", at, 0); !strings.Contains(other, "usage_note") {
		t.Fatalf("a new session should get the full header again, got %q", other)
	}
	if anonymous := provenanceHeaderForSession("", at, 0); strings.Contains(anonymous, "usage_note") || !strings.Contains(anonymous, "retrieved_at") {
		t.Fatalf("sessionless calls should use the compact header, got %q", anonymous)
	}
	if zero := provenanceHeaderForSession("session-a", time.Time{}, 0); zero != "" {
		t.Fatalf("zero timestamp = %q, want empty", zero)
	}
}
