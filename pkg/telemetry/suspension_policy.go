package telemetry

import "sync"

// suspensionPolicyMu guards the shared policy because telemetry events may be
// recorded from parallel provider goroutines.
var (
	suspensionPolicyMu sync.RWMutex
	suspensionPolicy   = DefaultSuspensionPolicy()
)

// SetSuspensionPolicy updates the process-wide circuit breaker policy used
// when deriving health. The zero policy restores defaults.
func SetSuspensionPolicy(policy SuspensionPolicy) {
	suspensionPolicyMu.Lock()
	suspensionPolicy = policy.Normalize()
	suspensionPolicyMu.Unlock()
}

// CurrentSuspensionPolicy returns a copy of the active policy.
func CurrentSuspensionPolicy() SuspensionPolicy {
	suspensionPolicyMu.RLock()
	defer suspensionPolicyMu.RUnlock()
	copied := suspensionPolicy
	times := make(map[string]int64, len(suspensionPolicy.SuspendedTimesMS))
	for k, v := range suspensionPolicy.SuspendedTimesMS {
		times[k] = v
	}
	copied.SuspendedTimesMS = times
	return copied
}
