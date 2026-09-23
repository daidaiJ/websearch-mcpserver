package telemetry

import "time"

// SuspensionPolicy controls how long a source is reported as suspended after
// consecutive failures. It never changes whether a call is attempted; the
// control center only reports the state.
type SuspensionPolicy struct {
	// BanTimeMS applies to error kinds without a specific suspended time.
	BanTimeMS int64
	// MaxBanTimeMS caps every suspension.
	MaxBanTimeMS int64
	// SuspendedTimesMS overrides the pause per error kind.
	SuspendedTimesMS map[string]int64
}

var defaultSuspendedTimes = map[string]int64{
	ErrorKindRateLimit:    180_000,
	ErrorKindCaptcha:      3_600_000,
	ErrorKindAccessDenied: 86_400_000,
	ErrorKindTimeout:      300_000,
	ErrorKindNetwork:      300_000,
	ErrorKindParse:        900_000,
	ErrorKindNoResult:     300_000,
}

// DefaultSuspensionPolicy mirrors SearXNG's ban_time_on_fail /
// max_ban_time_on_fail / suspended_times semantics.
func DefaultSuspensionPolicy() SuspensionPolicy {
	times := make(map[string]int64, len(defaultSuspendedTimes))
	for k, v := range defaultSuspendedTimes {
		times[k] = v
	}
	return SuspensionPolicy{BanTimeMS: 5_000, MaxBanTimeMS: 86_400_000, SuspendedTimesMS: times}
}

// Normalize fills in missing fields from the defaults so partial configs and
// hand-written policies behave predictably.
func (p SuspensionPolicy) Normalize() SuspensionPolicy {
	def := DefaultSuspensionPolicy()
	if p.BanTimeMS <= 0 {
		p.BanTimeMS = def.BanTimeMS
	}
	if p.MaxBanTimeMS <= 0 {
		p.MaxBanTimeMS = def.MaxBanTimeMS
	}
	if p.MaxBanTimeMS < p.BanTimeMS {
		p.MaxBanTimeMS = p.BanTimeMS
	}
	merged := make(map[string]int64, len(def.SuspendedTimesMS)+len(p.SuspendedTimesMS))
	for k, v := range def.SuspendedTimesMS {
		merged[k] = v
	}
	for k, v := range p.SuspendedTimesMS {
		if v > 0 {
			merged[k] = v
		}
	}
	p.SuspendedTimesMS = merged
	return p
}

// SuspensionState is the read-only circuit breaker result for one source.
type SuspensionState struct {
	Suspended     bool
	Until         time.Time
	BaseTimeMS    int64
	ErrorKind     string
	FailureStreak int
}

// EvaluateSuspension reports whether a source should currently be shown as
// suspended. consecutiveFailures counts the newest-first failure streak, and
// at least three failures are required before any suspension is reported.
// The newest failure's timestamp anchors the pause so a fresh failure always
// restarts the countdown.
func EvaluateSuspension(policy SuspensionPolicy, consecutiveFailures int, lastKind string, lastAt, now time.Time) SuspensionState {
	state := SuspensionState{ErrorKind: lastKind, FailureStreak: consecutiveFailures}
	if consecutiveFailures < 3 || lastAt.IsZero() {
		return state
	}
	policy = policy.Normalize()
	base := policy.BanTimeMS
	if override, ok := policy.SuspendedTimesMS[lastKind]; ok && override > 0 {
		base = override
	}
	if base > policy.MaxBanTimeMS {
		base = policy.MaxBanTimeMS
	}
	state.BaseTimeMS = base
	state.Until = lastAt.Add(time.Duration(base) * time.Millisecond)
	state.Suspended = state.Until.After(now)
	return state
}
