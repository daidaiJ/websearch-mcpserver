package memguard

import (
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeMode(t *testing.T) {
	cases := map[string]string{
		"":            ModeOff,
		"off":         ModeOff,
		"balanced":    ModeBalanced,
		" Aggressive": ModeAggressive,
		"unknown":     ModeOff,
	}
	for in, want := range cases {
		if got := NormalizeMode(in); got != want {
			t.Fatalf("NormalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func testController(now *time.Time, heap uint64, freed *int32) *Controller {
	return &Controller{
		mode:       ModeAggressive,
		idle:       10 * time.Second,
		minHeap:    8 << 20,
		checkEvery: time.Hour,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		freeOS:     func() { atomic.AddInt32(freed, 1) },
		now:        func() time.Time { return *now },
		heapAlloc:  func() uint64 { return heap },
	}
}

func TestTickWaitsForIdleWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	var freed int32
	c := testController(&now, 64<<20, &freed)

	if c.tick() {
		t.Fatal("tick before any activity must not free")
	}

	c.Begin()
	c.End()
	if c.tick() {
		t.Fatal("tick immediately after activity must not free")
	}

	now = now.Add(11 * time.Second)
	if !c.tick() {
		t.Fatal("idle window passed; expected a reclaim")
	}
	if got := atomic.LoadInt32(&freed); got != 1 {
		t.Fatalf("freeOS calls = %d, want 1", got)
	}
	if c.Stats().FreeOSTotal != 1 {
		t.Fatalf("stats = %+v", c.Stats())
	}
}

func TestTickSkipsWhileInflight(t *testing.T) {
	now := time.Unix(1000, 0)
	var freed int32
	c := testController(&now, 64<<20, &freed)
	c.Begin()
	c.End()
	now = now.Add(time.Minute)

	c.Begin()
	defer c.End()
	if c.tick() {
		t.Fatal("must not reclaim while a tool call is in flight")
	}
}

func TestTickSkipsSmallHeap(t *testing.T) {
	now := time.Unix(1000, 0)
	var freed int32
	c := testController(&now, 1<<20, &freed)
	c.Begin()
	c.End()
	now = now.Add(time.Minute)
	if c.tick() {
		t.Fatal("heap below the floor must not trigger a reclaim")
	}
}

func TestNilControllerIsSafe(t *testing.T) {
	var c *Controller
	c.Begin()
	c.End()
	if got := c.Stats(); got.Enabled {
		t.Fatalf("nil controller stats = %+v", got)
	}
}

func TestStartAppliesAndRestoresGCPercent(t *testing.T) {
	previous := debug.SetGCPercent(100)
	defer debug.SetGCPercent(previous)

	c := Start(Config{Mode: ModeAggressive})
	if c == nil {
		t.Fatal("aggressive mode must return a controller")
	}
	got := debug.SetGCPercent(100) // read back and restore 100 for cleanup
	if got != 40 {
		t.Fatalf("GC percent = %d, want 40", got)
	}
	Stop()
	if after := debug.SetGCPercent(100); after != 100 {
		t.Fatalf("GC percent after Stop = %d, want restored 100", after)
	}
}

func TestStartOffChangesNothing(t *testing.T) {
	if c := Start(Config{Mode: ModeOff}); c != nil {
		t.Fatalf("off mode returned %+v", c)
	}
}
