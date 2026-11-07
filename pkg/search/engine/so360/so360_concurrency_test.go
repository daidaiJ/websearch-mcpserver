package so360

import (
	"errors"
	"sync"
	"testing"

	"websearch/pkg/antirobot"
)

// ── B2/B12：so360 的轻形态（退避）+ 本端限流归因 ──────────────────────────────

// TestSo360RecordSuccessKeepsConcurrentFail 一次成功不得抹掉并发请求记录的失败（B2）。
func TestSo360RecordSuccessKeepsConcurrentFail(t *testing.T) {
	e := &so360Engine{opts: So360Opts{Enabled: true}}
	gen := e.stateGen()
	e.recordFail()
	e.recordSuccess(gen)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.consecFails == 0 || e.backoff == 0 {
		t.Errorf("并发失败后成功不应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

func TestSo360RecordSuccessClearsOwnFail(t *testing.T) {
	e := &so360Engine{opts: So360Opts{Enabled: true}}
	e.recordFail()
	e.recordSuccess(e.stateGen())

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 || e.consecFails != 0 {
		t.Errorf("成功应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

func TestSo360LocalRateLimitIsAttributable(t *testing.T) {
	e := &so360Engine{opts: So360Opts{Enabled: true}, limiter: antirobot.NewRateLimiter(1, 1)}
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

// TestSo360ConcurrentStateUpdates 并发读写退避状态不得有竞争。
func TestSo360ConcurrentStateUpdates(t *testing.T) {
	e := &so360Engine{opts: So360Opts{Enabled: true}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				gen := e.stateGen()
				e.recordFail()
				e.recordSuccess(gen)
			}
		}()
	}
	wg.Wait()
}
