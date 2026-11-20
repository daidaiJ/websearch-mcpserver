// Package admission bounds in-process concurrency with a buffered channel.
//
// A Gate holds at most capacity slots. Callers acquire a slot before doing the
// bounded work (an MCP tool call, an outbound search or fetch) and release it
// afterwards. When every slot is busy, a caller may wait for a short, bounded
// budget; once that budget expires the call is shed with ErrBusy instead of
// queueing indefinitely. The time budget is what keeps a thin VM from turning
// a burst of client calls into a burst of goroutines, sockets and buffers.
package admission

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrBusy reports that no slot became free within the configured wait budget.
var ErrBusy = errors.New("admission: no slot within wait budget")

// Stats is a point-in-time snapshot of one gate.
type Stats struct {
	Capacity int    // configured slots; 0 = unlimited
	Inflight int64  // slots currently held
	Queued   int64  // callers waiting for a slot
	Admitted uint64 // acquisitions granted since process start
	Shed     uint64 // acquisitions rejected since process start
}

// Gate is a buffered-channel semaphore with a bounded wait budget.
// The zero value and a nil *Gate both behave as "unlimited": Acquire succeeds
// immediately, so callers that were built before limits existed keep working.
type Gate struct {
	capacity int
	wait     time.Duration
	sem      chan struct{}

	inflight atomic.Int64
	queued   atomic.Int64
	admitted atomic.Uint64
	shed     atomic.Uint64
}

// NewGate builds a gate with capacity slots and a per-acquire wait budget.
// capacity <= 0 means unlimited and returns nil (nil-safe methods follow).
// A negative wait means "never queue": exhausted slots are shed immediately.
func NewGate(capacity int, wait time.Duration) *Gate {
	if capacity <= 0 {
		return nil
	}
	if wait < 0 {
		wait = 0
	}
	return &Gate{
		capacity: capacity,
		wait:     wait,
		sem:      make(chan struct{}, capacity),
	}
}

// Acquire takes a slot. The fast path is a single buffered-channel send; when
// the channel is full the caller waits at most g.wait, shortened by any
// earlier context deadline. The returned release is idempotent and must be
// called when the bounded work finishes.
func (g *Gate) Acquire(ctx context.Context) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case g.sem <- struct{}{}:
		g.inflight.Add(1)
		g.admitted.Add(1)
		return g.releaseFunc(), nil
	default:
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.wait == 0 {
		g.shed.Add(1)
		return nil, ErrBusy
	}

	wait := g.wait
	deadlineBound := false
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
			deadlineBound = true
		}
	}
	if wait <= 0 {
		g.shed.Add(1)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if deadlineBound {
			return nil, context.DeadlineExceeded
		}
		return nil, ErrBusy
	}

	g.queued.Add(1)
	defer g.queued.Add(-1)

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case g.sem <- struct{}{}:
		g.inflight.Add(1)
		g.admitted.Add(1)
		return g.releaseFunc(), nil
	case <-timer.C:
		g.shed.Add(1)
		// A shortened wait means the request deadline, not the budget, ended the
		// queue; surface the context error so callers can tell the two apart.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if deadlineBound {
			return nil, context.DeadlineExceeded
		}
		return nil, ErrBusy
	case <-ctx.Done():
		g.shed.Add(1)
		return nil, ctx.Err()
	}
}

func (g *Gate) releaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			g.inflight.Add(-1)
			<-g.sem
		})
	}
}

// Stats returns a snapshot. A nil gate (unlimited) reports zero capacity.
func (g *Gate) Stats() Stats {
	if g == nil {
		return Stats{}
	}
	return Stats{
		Capacity: g.capacity,
		Inflight: g.inflight.Load(),
		Queued:   g.queued.Load(),
		Admitted: g.admitted.Load(),
		Shed:     g.shed.Load(),
	}
}

type gateContextKey struct{}

// WithGate attaches an outbound gate to ctx. Nil gates leave ctx untouched, so
// the same code path serves limited and unlimited deployments.
func WithGate(ctx context.Context, g *Gate) context.Context {
	if g == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gateContextKey{}, g)
}

// FromContext returns the gate attached by WithGate, or nil.
func FromContext(ctx context.Context) *Gate {
	if ctx == nil {
		return nil
	}
	g, _ := ctx.Value(gateContextKey{}).(*Gate)
	return g
}

// Acquire takes a slot from the gate carried by ctx. Without a gate it is a
// no-op, which keeps direct callers and tests unbounded.
func Acquire(ctx context.Context) (func(), error) {
	if g := FromContext(ctx); g != nil {
		return g.Acquire(ctx)
	}
	return func() {}, nil
}
