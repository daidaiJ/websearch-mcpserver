package telemetry

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func shanghaiLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}
	return loc
}

// seededEvent mirrors what Record writes, but with a controlled timestamp so
// window and calendar-day tests stay deterministic.
type seededEvent struct {
	at       time.Time
	kind     string
	tool     string
	provider string
	success  bool
	results  int
}

func seedEvents(t *testing.T, s *Store, events ...seededEvent) {
	t.Helper()
	loc := shanghaiLocation(t)
	for _, e := range events {
		day := e.at.In(loc).Format("2006-01-02")
		if _, err := s.db.Exec(`INSERT INTO usage_events
			(occurred_at,day,kind,tool,provider,success,duration_ms,cache_hit,result_count)
			VALUES(?,?,?,?,?,?,0,0,?)`,
			e.at.Unix(), day, e.kind, e.tool, e.provider, boolInt(e.success), e.results); err != nil {
			t.Fatalf("seed usage_events: %v", err)
		}
		if _, err := s.db.Exec(`INSERT INTO daily_usage(day,kind,tool,provider,requests,successes,failures,result_count)
			VALUES(?,?,?,?,1,?,?,?)
			ON CONFLICT(day,kind,tool,provider) DO UPDATE SET
				requests=requests+1, successes=successes+excluded.successes,
				failures=failures+excluded.failures, result_count=result_count+excluded.result_count`,
			day, e.kind, e.tool, e.provider, boolInt(e.success), boolInt(!e.success), e.results); err != nil {
			t.Fatalf("seed daily_usage: %v", err)
		}
	}
}

func providerHealth(t *testing.T, s *Store, name string) Health {
	t.Helper()
	o, err := s.Overview()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range o.Providers {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("provider %q missing from overview", name)
	return Health{}
}

func recentFiltered(t *testing.T, s *Store, f EventFilter) []StoredEvent {
	t.Helper()
	events, err := s.RecentFiltered(f)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func requireAll(t *testing.T, label string, events []StoredEvent, ok func(StoredEvent) bool) {
	t.Helper()
	for _, e := range events {
		if !ok(e) {
			t.Fatalf("%s: unexpected event %+v", label, e)
		}
	}
}

func TestProviderNeedsThreeConsecutiveFailuresToBeDown(t *testing.T) {
	s := openTestStore(t)
	want := map[int]string{1: "degraded", 2: "degraded", 3: "down"}
	for i := 1; i <= 3; i++ {
		if err := s.Record(Event{Kind: "provider", Provider: "demo", Query: "private customer 123456 test", Success: false, Duration: time.Millisecond, Error: errors.New("upstream failed")}); err != nil {
			t.Fatal(err)
		}
		if got := providerHealth(t, s, "demo").Status; got != want[i] {
			t.Fatalf("after %d consecutive failures status = %q, want %q", i, got, want[i])
		}
	}
	if err := s.Record(Event{Kind: "provider", Provider: "demo", Success: true}); err != nil {
		t.Fatal(err)
	}
	if got := providerHealth(t, s, "demo").Status; got != "degraded" {
		t.Fatalf("status after a success ended the streak = %q, want degraded", got)
	}
}

func TestRecentWindowKeepsLastTwentyOldestToNewest(t *testing.T) {
	s := openTestStore(t)
	base := time.Now().Add(-25 * time.Minute)
	for i := 0; i < 25; i++ {
		seedEvents(t, s, seededEvent{
			at:       base.Add(time.Duration(i) * time.Minute),
			kind:     "provider",
			provider: "paged",
			success:  i%2 == 0,
		})
	}
	var want []bool
	for i := 5; i < 25; i++ { // newest 20 samples, oldest first
		want = append(want, i%2 == 0)
	}
	h := providerHealth(t, s, "paged")
	if h.SampleSize != 20 {
		t.Fatalf("sample size = %d, want 20", h.SampleSize)
	}
	if len(h.RecentOutcomes) != len(want) {
		t.Fatalf("recent outcomes = %d entries, want %d", len(h.RecentOutcomes), len(want))
	}
	for i := range want {
		if h.RecentOutcomes[i] != want[i] {
			t.Fatalf("recent outcomes[%d] = %v, want %v (full: %v)", i, h.RecentOutcomes[i], want[i], h.RecentOutcomes)
		}
	}
}

func TestProvidersUnseenFor24HoursAreUnknown(t *testing.T) {
	s := openTestStore(t)
	lastSeen := time.Now().Add(-25 * time.Hour)
	for i := 0; i < 3; i++ {
		seedEvents(t, s, seededEvent{
			at:       lastSeen.Add(-time.Duration(2-i) * time.Minute),
			kind:     "provider",
			provider: "stale-down",
			success:  false,
		})
	}
	seedEvents(t, s, seededEvent{at: lastSeen, kind: "provider", provider: "stale-ok", success: true})
	for _, name := range []string{"stale-down", "stale-ok"} {
		h := providerHealth(t, s, name)
		if h.Status != "unknown" {
			t.Fatalf("%s last seen %s has status %q, want unknown", name, h.LastSeenAt, h.Status)
		}
	}
}

func TestTodayUsesShanghaiCalendarDay(t *testing.T) {
	s := openTestStore(t)
	loc := shanghaiLocation(t)
	day := time.Now().In(loc)
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	yesterday := dayStart.Add(-time.Hour) // 昨日 23:00 +08

	if err := s.Record(Event{Kind: "tool", Tool: "now-check", Success: true}); err != nil {
		t.Fatal(err)
	}
	var storedDay string
	var ts int64
	if err := s.db.QueryRow(`SELECT day,occurred_at FROM usage_events WHERE tool='now-check'`).Scan(&storedDay, &ts); err != nil {
		t.Fatal(err)
	}
	if want := time.Unix(ts, 0).In(loc).Format("2006-01-02"); storedDay != want {
		t.Fatalf("stored day = %q, want shanghai day %q for %s (UTC %s)",
			storedDay, want, time.Unix(ts, 0).In(loc).Format(time.RFC3339), time.Unix(ts, 0).UTC().Format(time.RFC3339))
	}

	seedEvents(t, s,
		seededEvent{at: dayStart, kind: "tool", tool: "boundary-in", success: true, results: 3},
		seededEvent{at: yesterday, kind: "tool", tool: "boundary-out", success: true},
	)
	o, err := s.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if o.Today.Day != dayStart.Format("2006-01-02") {
		t.Fatalf("today = %q, want shanghai date %q", o.Today.Day, dayStart.Format("2006-01-02"))
	}
	// Midnight of the Shanghai day is still the previous UTC date, so a
	// UTC-based day boundary would drop boundary-in.
	if o.Today.Requests != 2 || o.Today.ResultCount != 3 {
		t.Fatalf("today requests=%d result_count=%d, want 2 and 3", o.Today.Requests, o.Today.ResultCount)
	}
	trend := map[string]int64{}
	for _, d := range o.Trend {
		trend[d.Day] = d.Requests
	}
	if got := trend[o.Today.Day]; got != 2 {
		t.Fatalf("trend for %s = %d requests, want 2", o.Today.Day, got)
	}
	if got := trend[dayStart.AddDate(0, 0, -1).Format("2006-01-02")]; got != 1 {
		t.Fatalf("trend for yesterday = %d requests, want 1", got)
	}

	events := recentFiltered(t, s, EventFilter{Limit: 1})
	if len(events) != 1 {
		t.Fatalf("recent(1) returned %d events", len(events))
	}
	if !strings.HasSuffix(events[0].OccurredAt, "+08:00") {
		t.Fatalf("occurred_at %q is not rendered in +08:00", events[0].OccurredAt)
	}
	parsed, err := time.Parse(time.RFC3339, events[0].OccurredAt)
	if err != nil || !parsed.Equal(time.Unix(ts, 0)) {
		t.Fatalf("occurred_at %q does not round-trip: %v", events[0].OccurredAt, err)
	}
}

func TestProviderFanOutDoesNotCountAsToolUsage(t *testing.T) {
	s := openTestStore(t)
	if err := s.Record(Event{Kind: "tool", Tool: "smartsearch", Success: true, Duration: 120 * time.Millisecond, ResultCount: 5}); err != nil {
		t.Fatal(err)
	}
	fanOut := []struct {
		name    string
		ok      bool
		results int
	}{{"doubao", true, 8}, {"jina", false, 0}, {"exa", true, 4}}
	for _, p := range fanOut {
		if err := s.Record(Event{Kind: "provider", Provider: p.name, Success: p.ok, Duration: 40 * time.Millisecond, ResultCount: p.results}); err != nil {
			t.Fatal(err)
		}
	}
	o, err := s.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if o.Today.Requests != 1 || o.Today.Successes != 1 || o.Today.Failures != 0 || o.Today.ResultCount != 5 {
		t.Fatalf("tool KPI absorbed provider fan-out: %+v", o.Today)
	}
	if len(o.Tools) != 1 || o.Tools[0].Name != "smartsearch" {
		t.Fatalf("tools = %+v, want only smartsearch", o.Tools)
	}
	if len(o.Providers) != 3 {
		t.Fatalf("providers = %d, want 3", len(o.Providers))
	}
	for _, h := range o.Providers {
		if h.Today.Requests != 1 {
			t.Fatalf("provider %s today requests = %d, want 1", h.Name, h.Today.Requests)
		}
	}
	var trendRequests int64
	for _, d := range o.Trend {
		trendRequests += d.Requests
	}
	if trendRequests != 1 {
		t.Fatalf("trend counted %d requests, want 1 tool request", trendRequests)
	}
}

func TestRecentFilteredByKindStatusSourceAndLimit(t *testing.T) {
	s := openTestStore(t)
	base := time.Now().Add(-time.Hour)
	seedEvents(t, s,
		seededEvent{at: base.Add(1 * time.Minute), kind: "tool", tool: "smartsearch", success: true},
		seededEvent{at: base.Add(2 * time.Minute), kind: "tool", tool: "cleanfetch", success: false},
		seededEvent{at: base.Add(3 * time.Minute), kind: "provider", provider: "doubao", success: true},
		seededEvent{at: base.Add(4 * time.Minute), kind: "provider", provider: "jina", success: false},
		seededEvent{at: base.Add(5 * time.Minute), kind: "tool", tool: "smartsearch", success: false},
		seededEvent{at: base.Add(6 * time.Minute), kind: "provider", provider: "doubao", success: false},
	)

	tools := recentFiltered(t, s, EventFilter{Kind: "tool"})
	if len(tools) != 3 {
		t.Fatalf("kind=tool returned %d events, want 3", len(tools))
	}
	requireAll(t, "kind=tool", tools, func(e StoredEvent) bool { return e.Kind == "tool" })

	failures := recentFiltered(t, s, EventFilter{Status: "failure"})
	if len(failures) != 4 {
		t.Fatalf("status=failure returned %d events, want 4", len(failures))
	}
	requireAll(t, "status=failure", failures, func(e StoredEvent) bool { return !e.Success })

	doubao := recentFiltered(t, s, EventFilter{Kind: "provider", Source: "doubao"})
	if len(doubao) != 2 {
		t.Fatalf("provider doubao returned %d events, want 2", len(doubao))
	}
	requireAll(t, "provider doubao", doubao, func(e StoredEvent) bool { return e.Provider == "doubao" })

	failedSearch := recentFiltered(t, s, EventFilter{Kind: "tool", Status: "failure", Source: "smartsearch"})
	if len(failedSearch) != 1 {
		t.Fatalf("smartsearch failures returned %d events, want 1", len(failedSearch))
	}
	requireAll(t, "smartsearch failures", failedSearch, func(e StoredEvent) bool {
		return e.Tool == "smartsearch" && !e.Success
	})

	anyKind := recentFiltered(t, s, EventFilter{Source: "doubao"})
	if len(anyKind) != 2 {
		t.Fatalf("source=doubao without kind returned %d events, want 2", len(anyKind))
	}

	limited := recentFiltered(t, s, EventFilter{Limit: 2})
	if len(limited) != 2 {
		t.Fatalf("limit=2 returned %d events", len(limited))
	}
	if limited[0].Provider != "doubao" || limited[0].Success || limited[1].Tool != "smartsearch" || limited[1].Success {
		t.Fatalf("limit=2 did not return the two newest events first: %+v", limited)
	}
}

func TestRawQueryIsNeverStored(t *testing.T) {
	s := openTestStore(t)
	query := "alice@example.com secret-token-abcdefgh customer 123456"
	if err := s.Record(Event{Kind: "tool", Tool: "smartsearch", Query: query, Success: true}); err != nil {
		t.Fatal(err)
	}
	events, err := s.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	joined := events[0].QueryKeywords + events[0].ErrorSummary + events[0].QueryTopic
	for _, secret := range []string{"alice@example.com", "secret-token-abcdefgh", "123456"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("stored sensitive query fragment %q", secret)
		}
	}
	if events[0].QueryHash == "" {
		t.Fatal("expected irreversible query hash")
	}
}
