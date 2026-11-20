package admission

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNilGateIsUnlimited(t *testing.T) {
	var g *Gate
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("nil gate must admit: %v", err)
	}
	release()
	if s := g.Stats(); s.Capacity != 0 || s.Inflight != 0 {
		t.Fatalf("nil gate stats = %+v", s)
	}
}

func TestGateShedsWithoutWaitWhenFull(t *testing.T) {
	g := NewGate(1, -1)
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	if _, err := g.Acquire(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second acquire err = %v, want ErrBusy", err)
	}
	if s := g.Stats(); s.Inflight != 1 || s.Shed != 1 || s.Admitted != 1 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestGateWaitsForReleaseWithinBudget(t *testing.T) {
	g := NewGate(1, time.Second)
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		release()
	}()

	second, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("queued acquire: %v", err)
	}
	second()
	if s := g.Stats(); s.Admitted != 2 || s.Shed != 0 || s.Inflight != 0 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestGateShedsWhenBudgetExpires(t *testing.T) {
	g := NewGate(1, 30*time.Millisecond)
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	start := time.Now()
	if _, err := g.Acquire(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("wait budget not honored: %s", elapsed)
	}
}

func TestGateHonorsContextDeadline(t *testing.T) {
	g := NewGate(1, time.Second)
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := g.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("deadline ignored: %s", elapsed)
	}
}

func TestGateReleaseIsIdempotent(t *testing.T) {
	g := NewGate(1, 0)
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
	release()

	again, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("reacquire after double release: %v", err)
	}
	again()
	if s := g.Stats(); s.Inflight != 0 {
		t.Fatalf("double release corrupted inflight: %+v", s)
	}
}

func TestGateConcurrentAcquireRespectsCapacity(t *testing.T) {
	const capacity = 3
	g := NewGate(capacity, time.Second)

	var current, peak int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.Acquire(context.Background())
			if err != nil {
				return
			}
			defer release()
			mu.Lock()
			current++
			if current > peak {
				peak = current
			}
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			mu.Lock()
			current--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if peak > capacity {
		t.Fatalf("peak concurrency = %d, capacity = %d", peak, capacity)
	}
	if s := g.Stats(); s.Inflight != 0 || s.Shed != 0 {
		t.Fatalf("stats after drain = %+v", s)
	}
}

func TestContextGateHelpers(t *testing.T) {
	ctx := context.Background()
	if g := FromContext(ctx); g != nil {
		t.Fatal("empty context must not carry a gate")
	}
	release, err := Acquire(ctx)
	if err != nil {
		t.Fatalf("no-gate acquire: %v", err)
	}
	release()

	g := NewGate(1, 0)
	ctx = WithGate(ctx, g)
	if FromContext(ctx) != g {
		t.Fatal("gate not attached to context")
	}
	held, err := Acquire(ctx)
	if err != nil {
		t.Fatalf("context gate acquire: %v", err)
	}
	if _, err := Acquire(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("context gate must shed when full: %v", err)
	}
	held()
}
