// Package memguard applies an opt-in memory policy for small hosts: a lower GC
// target, an optional soft heap limit, and returning free heap to the OS once
// the process goes idle.
//
// The default (mode off) leaves Go's runtime settings untouched. "balanced" and
// "aggressive" trade a small amount of CPU for a lower resident set: they lower
// GOGC, optionally set a soft memory limit, and call debug.FreeOSMemory only
// when no tool call is in flight and the configured idle window has passed.
package memguard

import (
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mode selects the memory policy.
const (
	ModeOff        = "off"
	ModeBalanced   = "balanced"
	ModeAggressive = "aggressive"
)

// Config is the memory policy as read from config.yaml.
type Config struct {
	Mode    string // off (default) / balanced / aggressive
	LimitMB int    // >0 sets a soft memory limit (GOMEMLIMIT semantics)
}

type preset struct {
	gcPercent int
	idle      time.Duration
	minHeap   uint64
}

var presets = map[string]preset{
	ModeBalanced: {gcPercent: 75, idle: 60 * time.Second, minHeap: 16 << 20},
	// Go's own runtime baseline sits around 4-6 MB HeapAlloc, so a higher floor
	// would make aggressive mode a no-op on a quiet thin VM.
	ModeAggressive: {gcPercent: 40, idle: 10 * time.Second, minHeap: 4 << 20},
}

// NormalizeMode maps unknown/empty values to off.
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeBalanced:
		return ModeBalanced
	case ModeAggressive:
		return ModeAggressive
	default:
		return ModeOff
	}
}

// Stats is a point-in-time snapshot for /metrics.
type Stats struct {
	Mode        string
	Enabled     bool
	Inflight    int64
	FreeOSTotal uint64
}

// Controller owns the process-wide runtime knobs and the idle reclaimer.
type Controller struct {
	mode       string
	idle       time.Duration
	minHeap    uint64
	checkEvery time.Duration

	prevGCPercent int
	prevMemLimit  int64

	inflight     atomic.Int64
	lastActivity atomic.Int64 // unix nano; 0 = no traffic yet
	frees        atomic.Uint64

	stop      chan struct{}
	done      chan struct{}
	freeOS    func()
	now       func() time.Time
	heapAlloc func() uint64
}

var (
	currentMu sync.Mutex
	current   *Controller
)

// Start applies the configured policy and starts the idle reclaimer. It is
// idempotent enough for the daemon: a previously started controller is stopped
// first. Mode off (or unknown) returns nil and changes nothing.
func Start(cfg Config) *Controller {
	mode := NormalizeMode(cfg.Mode)
	if mode == ModeOff {
		Stop()
		return nil
	}
	p := presets[mode]

	currentMu.Lock()
	defer currentMu.Unlock()
	if current != nil {
		current.stopLocked()
	}

	c := &Controller{
		mode:       mode,
		idle:       p.idle,
		minHeap:    p.minHeap,
		checkEvery: clampDuration(p.idle/4, time.Second, 15*time.Second),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		freeOS:     debug.FreeOSMemory,
		now:        time.Now,
		heapAlloc: func() uint64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return m.HeapAlloc
		},
	}
	// Stop flags are process-global: remember the previous values so tests and
	// embedded users can restore them on Stop.
	c.prevGCPercent = debug.SetGCPercent(p.gcPercent)
	if cfg.LimitMB > 0 {
		c.prevMemLimit = debug.SetMemoryLimit(int64(cfg.LimitMB) << 20)
	} else {
		c.prevMemLimit = -1
	}
	c.lastActivity.Store(0)

	current = c
	go c.run()
	return c
}

// Stop terminates the current controller, if any, and restores runtime knobs.
func Stop() {
	currentMu.Lock()
	defer currentMu.Unlock()
	if current != nil {
		current.stopLocked()
		current = nil
	}
}

func (c *Controller) stopLocked() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	<-c.done
	if c.prevGCPercent >= 0 {
		debug.SetGCPercent(c.prevGCPercent)
	}
	if c.prevMemLimit > 0 {
		debug.SetMemoryLimit(c.prevMemLimit)
	}
}

// Begin marks the start of a tool call; End marks its completion and the last
// activity timestamp used by the idle reclaimer.
func (c *Controller) Begin() {
	if c == nil {
		return
	}
	c.inflight.Add(1)
}

// End decrements the in-flight counter and records activity.
func (c *Controller) End() {
	if c == nil {
		return
	}
	c.inflight.Add(-1)
	c.lastActivity.Store(c.now().UnixNano())
}

// Stats returns counters for /metrics. A nil controller reports disabled.
func (c *Controller) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	return Stats{
		Mode:        c.mode,
		Enabled:     true,
		Inflight:    c.inflight.Load(),
		FreeOSTotal: c.frees.Load(),
	}
}

func (c *Controller) run() {
	defer close(c.done)
	ticker := time.NewTicker(c.checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.tick()
		}
	}
}

// tick reclaims OS memory when the process is idle and the heap is above the
// floor. It returns true when FreeOSMemory ran.
func (c *Controller) tick() bool {
	if c.inflight.Load() > 0 {
		return false
	}
	last := c.lastActivity.Load()
	if last == 0 {
		return false
	}
	now := c.now()
	if now.Sub(time.Unix(0, last)) < c.idle {
		return false
	}
	if c.heapAlloc() < c.minHeap {
		return false
	}
	c.freeOS()
	c.frees.Add(1)
	// Reset the idle clock so a still-high heap does not trigger a second full
	// GC on consecutive ticks; the next reclaim waits for another idle window.
	c.lastActivity.Store(now.UnixNano())
	return true
}

func clampDuration(d, min, max time.Duration) time.Duration {
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}

// CurrentStats exposes the active controller to /metrics without threading it
// through the HTTP layer. A stopped or disabled policy reports Enabled=false.
func CurrentStats() Stats {
	currentMu.Lock()
	defer currentMu.Unlock()
	if current == nil {
		return Stats{}
	}
	return current.Stats()
}
