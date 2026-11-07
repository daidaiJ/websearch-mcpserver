package google

import (
	"errors"
	"sync"
	"testing"

	"websearch/pkg/antirobot"
)

// ── B1：会话轮换的状态迁移必须原子 ────────────────────────────────────────────

func TestGoogleConcurrentSessionRotation(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{Enabled: true}, limiter: antirobot.NewRateLimiter(100, 1000)}

	const workers, perWorker = 8, 200
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range perWorker {
				e.rotateSessionIfNeeded()
				e.takeSession()
			}
		}()
	}
	close(start)
	wg.Wait()

	e.mu.Lock()
	reqCount := e.reqCount
	e.mu.Unlock()
	if reqCount >= googleSessionLifetime {
		t.Errorf("reqCount 应始终小于会话寿命 %d, got %d", googleSessionLifetime, reqCount)
	}
}

// ── B2：一次成功不得抹掉并发失败 ──────────────────────────────────────────────

func TestGoogleRecordSuccessKeepsConcurrentFail(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{Enabled: true}}
	gen := e.stateGen()
	e.recordFail()
	e.recordSuccess(gen)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.consecFails == 0 || e.backoff == 0 {
		t.Errorf("并发失败后成功不应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

func TestGoogleRecordSuccessClearsOwnFail(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{Enabled: true}}
	e.recordFail()
	e.recordSuccess(e.stateGen())

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 || e.consecFails != 0 {
		t.Errorf("成功应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

// ── B12：本端限流必须可归因 ───────────────────────────────────────────────────

func TestGoogleLocalRateLimitIsAttributable(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{Enabled: true}, limiter: antirobot.NewRateLimiter(1, 1)}
	if !e.limiter.Allow() {
		t.Fatal("首次应放行")
	}

	resp, err := e.Search("q", 1, antirobot.TimeRangeNone)
	if err == nil {
		t.Fatal("本端限流必须返回错误（旧实现返回空结果 + nil）")
	}
	if !errors.Is(err, antirobot.ErrLocalRateLimit) {
		t.Fatalf("错误应包裹本端限流哨兵, got %v", err)
	}
	if resp == nil || len(resp.Results) != 0 {
		t.Fatalf("本端限流应返回空结果, got %+v", resp)
	}
}
