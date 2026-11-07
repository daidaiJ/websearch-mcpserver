package bing

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"websearch/pkg/antirobot"
)

type hangRT struct{}

func (hangRT) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// ── B1：会话轮换的状态迁移必须原子 ────────────────────────────────────────────

// TestBingConcurrentSessionRotation 并发驱动会话轮换：旧实现在锁外写
// client / ua / reqCount，`-race` 会报 rotateSession 与请求路径的数据竞争。
func TestBingConcurrentSessionRotation(t *testing.T) {
	e := &bingEngine{opts: BingOpts{Enabled: true}, limiter: antirobot.NewRateLimiter(100, 1000)}

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
	if reqCount >= bingSessionLifetime {
		t.Errorf("reqCount 应始终小于会话寿命 %d, got %d", bingSessionLifetime, reqCount)
	}
}

// ── B2：一次成功不得抹掉并发失败 ──────────────────────────────────────────────

func TestBingRecordSuccessKeepsConcurrentFail(t *testing.T) {
	e := &bingEngine{opts: BingOpts{Enabled: true}}
	gen := e.stateGen()
	e.recordFail()
	e.recordSuccess(gen)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.consecFails == 0 || e.backoff == 0 {
		t.Errorf("并发失败后成功不应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

func TestBingRecordSuccessClearsOwnFail(t *testing.T) {
	e := &bingEngine{opts: BingOpts{Enabled: true}}
	e.recordFail()
	e.recordSuccess(e.stateGen())

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 || e.consecFails != 0 {
		t.Errorf("成功应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

// ── B12：本端限流必须可归因 ───────────────────────────────────────────────────

func TestBingLocalRateLimitIsAttributable(t *testing.T) {
	e := &bingEngine{opts: BingOpts{Enabled: true}, limiter: antirobot.NewRateLimiter(1, 1)}
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

// TestBingSoftTimeoutDoesNotAdvanceState ctx 到期时被放弃的调用不得推进退避（B4）。
func TestBingSoftTimeoutDoesNotAdvanceState(t *testing.T) {
	origDelay, origJitter := bingBaseDelay, bingJitter
	bingBaseDelay, bingJitter = time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		bingBaseDelay, bingJitter = origDelay, origJitter
	})

	e := NewBing(BingOpts{Enabled: true, PerSec: 1000, PerMin: 10000}).(*bingEngine)
	e.mu.Lock()
	e.client = &http.Client{Transport: hangRT{}}
	e.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	e.mu.Lock()
	epochBefore := e.stateEpoch
	e.mu.Unlock()

	_, err := e.SearchContext(ctx, "q", 1, antirobot.TimeRangeNone)
	if err == nil {
		t.Fatal("ctx 到期后应返回错误")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stateEpoch != epochBefore {
		t.Errorf("被放弃的调用不应推进状态代次, %d → %d", epochBefore, e.stateEpoch)
	}
	if e.backoff != 0 || e.consecFails != 0 {
		t.Errorf("被放弃的调用不应推进退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}
