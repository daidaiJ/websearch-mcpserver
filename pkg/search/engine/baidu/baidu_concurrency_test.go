package baidu

import (
	"errors"
	"sync"
	"testing"

	"websearch/pkg/antirobot"
)

// ── B1：会话轮换的状态迁移必须原子（client / ua / reqCount 一起入锁） ──────────

// TestBaiduConcurrentSessionRotation 并发驱动会话轮换：旧实现在锁外写
// client / ua / reqCount，`-race` 会报 rotateSession 与请求路径的数据竞争。
// 本用例直接驱动轮换路径，不依赖网络。
func TestBaiduConcurrentSessionRotation(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Enabled: true}, limiter: antirobot.NewRateLimiter(100, 1000)}

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
	if reqCount >= baiduSessionLifetime {
		t.Errorf("reqCount 应始终小于会话寿命 %d, got %d", baiduSessionLifetime, reqCount)
	}
}

// TestBaiduRecordSuccessKeepsConcurrentFail 一次成功不得抹掉并发请求记录的失败（B2）。
func TestBaiduRecordSuccessKeepsConcurrentFail(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Enabled: true}}

	gen := e.stateGen()
	e.recordFail()
	e.recordSuccess(gen)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.consecFails == 0 || e.backoff == 0 {
		t.Errorf("并发失败后成功不应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

// TestBaiduRecordSuccessClearsOwnFail 无并发写入时，成功仍应清退避。
func TestBaiduRecordSuccessClearsOwnFail(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Enabled: true}}
	e.recordFail()
	gen := e.stateGen()
	e.recordSuccess(gen)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 || e.consecFails != 0 {
		t.Errorf("成功应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

// ── B12：本端限流必须可归因 ───────────────────────────────────────────────────

// TestBaiduLocalRateLimitIsAttributable 本端限流返回哨兵错误而非「空结果 + nil」：
// 旧实现返回空结果 + nil，编排层记成「成功但零结果」，限流这一类在归因里消失。
func TestBaiduLocalRateLimitIsAttributable(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Enabled: true}, limiter: antirobot.NewRateLimiter(1, 1)}
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
