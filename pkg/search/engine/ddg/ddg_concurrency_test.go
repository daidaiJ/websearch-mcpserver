package ddg

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"websearch/pkg/antirobot"
)

// rotateSessionForTest 单测用：经同一临界区触发一次轮换。
func (e *ddgEngine) rotateSessionForTest() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rotateSessionLocked()
}

// ── B1：会话轮换的状态迁移必须原子（client / ua / reqCount 一起入锁） ──────────

// TestConcurrentSessionRotation_NoRace 并发驱动会话轮换与请求发起：
// 轮换会重建 client 与 UA，若轮换在锁外写这些字段（旧实现），
// `go test -race` 会报 rotateSession 与 takeSession/setHeaders 的数据竞争。
func TestConcurrentSessionRotation_NoRace(t *testing.T) {
	withDDGTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, okHTML)
	})

	e := cooldownTestEngine()
	// 本用例只验证轮换的并发安全：放开本端限流（否则 320 次连发会被限流器拦下）
	e.limiter = antirobot.NewRateLimiter(1_000_000, 1_000_000)

	const workers = 8
	const perWorker = 40
	var wg sync.WaitGroup
	var failures atomic.Int32
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range perWorker {
				if _, err := e.Search("q", 1, antirobot.TimeRangeNone); err != nil {
					if failures.Add(1) == 1 {
						t.Logf("首个失败: %v", err)
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	if n := failures.Load(); n != 0 {
		t.Fatalf("并发搜索不应失败, failures=%d", n)
	}
	// 计数器不变量：任何时刻都不得超过会话寿命（轮换在锁内完成时成立）。
	e.mu.Lock()
	reqCount := e.reqCount
	e.mu.Unlock()
	if reqCount >= ddgSessionLifetime {
		t.Errorf("reqCount 应始终小于会话寿命 %d, got %d", ddgSessionLifetime, reqCount)
	}
}

// TestSessionSnapshotIsConsistent 会话快照读取 client 与 UA 必须来自同一次临界区：
// 快照里的 UA 必须与引擎当前 UA 对应（同一代）。轮换后重新取快照必须看到新状态。
func TestSessionSnapshotIsConsistent(t *testing.T) {
	e := cooldownTestEngine()
	before := e.takeSession()

	e.rotateSessionForTest()
	after := e.takeSession()

	if before.client == after.client {
		t.Fatal("轮换后客户端应被重建")
	}
	e.mu.Lock()
	curUA := e.ua
	e.mu.Unlock()
	if after.ua != curUA {
		t.Errorf("快照 UA 应与引擎当前 UA 一致, snapshot=%q current=%q", after.ua, curUA)
	}
}

// ── B2：一次成功不得清空并发建立的冷却 ────────────────────────────────────────

// TestRecordSuccessKeepsConcurrentCooldown 请求开始后、成功返回前，另一个并发
// 请求进入冷却；本次成功不得清掉那个冷却（旧实现无条件清零）。
func TestRecordSuccessKeepsConcurrentCooldown(t *testing.T) {
	withShortCooldown(t, 20*time.Second)
	e := cooldownTestEngine()

	// 本请求开始，取状态代次快照
	gen := e.stateGen()

	// 并在请求期间：并发请求撞上游 202，进入冷却
	e.enterCooldown(0)
	if e.cooldownRemaining() <= 0 {
		t.Fatal("并发请求应已建立冷却")
	}

	// 本请求成功返回
	e.recordSuccess(gen)

	if left := e.cooldownRemaining(); left <= 0 {
		t.Fatalf("并发建立的冷却不应被一次成功清空, remaining=%v", left)
	}
}

// TestRecordSuccessClearsOwnCooldown 无人并发改写时，本请求成功仍应清掉
// 此前累积的冷却与退避（快照之后没有其它写入）。
func TestRecordSuccessClearsOwnCooldown(t *testing.T) {
	withShortCooldown(t, 20*time.Second)
	e := cooldownTestEngine()

	e.enterCooldown(0)
	e.recordFail()

	// 本请求开始：此后无人写入
	gen := e.stateGen()
	e.recordSuccess(gen)

	if left := e.cooldownRemaining(); left != 0 {
		t.Errorf("成功应清掉冷却, remaining=%v", left)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 || e.consecFails != 0 {
		t.Errorf("成功应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

// TestRecordSuccessBlockedByConcurrentFail 快照之后若发生并发失败，
// 本请求成功不得清退避（否则会抹掉并发请求观察到的上游异常）。
func TestRecordSuccessBlockedByConcurrentFail(t *testing.T) {
	e := cooldownTestEngine()

	gen := e.stateGen()
	e.recordFail()
	e.recordSuccess(gen)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.consecFails == 0 || e.backoff == 0 {
		t.Errorf("并发失败后成功不应清退避, backoff=%v consecFails=%d", e.backoff, e.consecFails)
	}
}

// TestConcurrentSuccessDoesNotClearCooldown 端到端并发形态：一个请求撞 202 进冷却，
// 同时另一个请求拿到 200 成功返回；冷却必须仍然有效，且冷却期内不再打上游。
func TestConcurrentSuccessDoesNotClearCooldown(t *testing.T) {
	withShortCooldown(t, 30*time.Second)

	var hits atomic.Int32
	// 先放一个 202 让并发请求进入冷却，随后全部返回 200
	withDDGTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		fmt.Fprint(w, okHTML)
	})

	e := cooldownTestEngine()
	// 两个并发请求：一个必然撞限流（首次 202），另一个成功
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.Search("q", 1, antirobot.TimeRangeNone)
		}()
	}
	wg.Wait()

	if left := e.cooldownRemaining(); left <= 0 {
		t.Fatal("并发成功不应清掉另一次请求建立的冷却")
	}
	// 冷却期内再搜索：必须快速失败，不再打上游
	before := hits.Load()
	if _, err := e.Search("q2", 1, antirobot.TimeRangeNone); err == nil || !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("冷却期内应快速失败, got %v", err)
	}
	if after := hits.Load(); after != before {
		t.Fatalf("冷却期内不应请求上游, hits %d → %d", before, after)
	}
}

// ── B6：冷却档位的「读-改-写」必须原子 ────────────────────────────────────────

// TestConcurrentCooldownEscalation 并发进入冷却时，档位升级不得丢失：
// 旧实现把档位读在锁外，并发调用可能都读到同一档位，后写者用 base*2 覆盖
// 已升到更高档位的冷却。
func TestConcurrentCooldownEscalation(t *testing.T) {
	withShortCooldown(t, time.Millisecond)
	ddgRateCooldownMax = time.Hour // 放大封顶，避免饱和掩盖丢失的升级

	e := cooldownTestEngine()

	const workers = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			e.enterCooldown(0)
		}()
	}
	close(start)
	wg.Wait()

	e.mu.Lock()
	got := e.cooldown
	e.mu.Unlock()

	// 串行化的 32 次升级应至少跨过好几档；旧实现常止步于 base*2。
	if got < 4*time.Millisecond {
		t.Fatalf("并发冷却应逐级翻倍, got %v（旧实现会丢失升级）", got)
	}
}

// ── B4：软超时放弃后不再推进共享状态 ─────────────────────────────────────────

// TestSoftTimeoutDoesNotAdvanceState 请求 ctx 到期（编排层软超时/客户端断连）时，
// 被放弃的调用不得再推进退避状态（旧实现会 recordFail 推进 backoff 与连续失败数）。
func TestSoftTimeoutDoesNotAdvanceState(t *testing.T) {
	release := make(chan struct{})
	withDDGTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release // 故意挂住，让客户端先超时
	})
	t.Cleanup(func() { close(release) })

	e := cooldownTestEngine()
	e.opts.Timeout = 200 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
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

// TestDDGLocalRateLimitIsAttributable DDG 本端限流走 LocalLimitSkipped 哨兵，
// 与其它引擎同一归因路径（旧实现是裸 fmt.Errorf）。
func TestDDGLocalRateLimitIsAttributable(t *testing.T) {
	e := cooldownTestEngine()
	e.limiter = antirobot.NewRateLimiter(1, 1)
	if !e.limiter.Allow() {
		t.Fatal("首次应放行")
	}

	resp, err := e.Search("q", 1, antirobot.TimeRangeNone)
	if err == nil {
		t.Fatal("本端限流必须返回错误")
	}
	if !errors.Is(err, antirobot.ErrLocalRateLimit) {
		t.Fatalf("错误应包裹本端限流哨兵, got %v", err)
	}
	if resp == nil || len(resp.Results) != 0 {
		t.Fatalf("本端限流应返回空结果, got %+v", resp)
	}
}
