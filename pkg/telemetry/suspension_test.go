package telemetry

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestEvaluateSuspensionRequiresStreakAndHonorsErrorKind(t *testing.T) {
	policy := DefaultSuspensionPolicy()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Second)

	if state := EvaluateSuspension(policy, 2, ErrorKindRateLimit, last, now); state.Suspended {
		t.Fatal("two failures must not suspend a source")
	}
	rate := EvaluateSuspension(policy, 3, ErrorKindRateLimit, last, now)
	if !rate.Suspended {
		t.Fatal("three rate-limit failures should suspend")
	}
	if rate.BaseTimeMS != policy.SuspendedTimesMS[ErrorKindRateLimit] {
		t.Fatalf("rate limit pause = %d, want %d", rate.BaseTimeMS, policy.SuspendedTimesMS[ErrorKindRateLimit])
	}
	denied := EvaluateSuspension(policy, 3, ErrorKindAccessDenied, last, now)
	if denied.BaseTimeMS <= rate.BaseTimeMS {
		t.Fatalf("access denied pause %d should exceed rate limit pause %d", denied.BaseTimeMS, rate.BaseTimeMS)
	}
	expired := EvaluateSuspension(policy, 3, ErrorKindRateLimit, now.Add(-time.Hour), now)
	if expired.Suspended {
		t.Fatal("suspension should expire once the window passes")
	}
}

func TestSuspensionClearsAfterSuccess(t *testing.T) {
	SetSuspensionPolicy(DefaultSuspensionPolicy())
	t.Cleanup(func() { SetSuspensionPolicy(DefaultSuspensionPolicy()) })

	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for i := 0; i < 3; i++ {
		record := Event{Kind: "provider", Provider: "rate-limited", Query: "q", Success: false, Error: errors.New("HTTP 429 too many requests")}
		if err := store.Record(record); err != nil {
			t.Fatal(err)
		}
	}
	health, err := store.health("provider", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(health) != 1 || health[0].SuspendedUntil == "" || health[0].SuspendReason != ErrorKindRateLimit {
		t.Fatalf("expected suspended state, got %+v", health)
	}
	if err := store.Record(Event{Kind: "provider", Provider: "rate-limited", Success: true}); err != nil {
		t.Fatal(err)
	}
	health, err = store.health("provider", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health[0].SuspendedUntil != "" || health[0].SuspendReason != "" {
		t.Fatalf("success should clear suspension, got %+v", health[0])
	}
}
