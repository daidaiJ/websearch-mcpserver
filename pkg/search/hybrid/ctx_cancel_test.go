package hybrid

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"websearch/pkg/search/core"
)

// mutatingSlowEngine 在软超时被放弃后仍会继续推进自己的共享状态，
// 用于验证编排层把 ctx 传下去后引擎能观察到取消。
type mutatingSlowEngine struct {
	name      string
	mutations atomic.Int32
	ctxSeen   chan context.Context
}

func (e *mutatingSlowEngine) Name() string                        { return e.name }
func (e *mutatingSlowEngine) Search(query string) (string, error) { return "", nil }
func (e *mutatingSlowEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	// 旧接口：没有 ctx，被放弃后必然继续推进
	e.mutations.Add(1)
	return nil, context.DeadlineExceeded
}
func (e *mutatingSlowEngine) MergeContent(_ string, _ []core.SearchResult) (string, error) {
	return "", nil
}
func (e *mutatingSlowEngine) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	if e.ctxSeen != nil {
		select {
		case e.ctxSeen <- ctx:
		default:
		}
	}
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		// ctx 取消：不再推进共享状态（B4）
		return nil, ctx.Err()
	}
	e.mutations.Add(1)
	return nil, context.DeadlineExceeded
}

// TestHybridSearch_SoftTimeoutCancelsEngineCtx 软超时后编排层必须取消下传的 ctx，
// 使实现了 ContextSearcher 的引擎能停止在飞工作并放弃推进共享状态（B4）。
func TestHybridSearch_SoftTimeoutCancelsEngineCtx(t *testing.T) {
	got := make(chan context.Context, 1)
	slow := &mutatingSlowEngine{name: "slow", ctxSeen: got}
	fast := &ctxAwareEngine{name: "fast", results: []core.SearchResult{
		{Title: "ok", Url: "https://example.com/ok", Engine: "fast"},
	}}
	hs := NewHybridSearch(fast, slow)
	hs.SetEngineTimeout(50 * time.Millisecond)

	if _, _, err := hs.SearchRawDiagnostics("q"); err != nil {
		t.Fatalf("快引擎结果应正常返回, err=%v", err)
	}

	var engineCtx context.Context
	select {
	case engineCtx = <-got:
	case <-time.After(time.Second):
		t.Fatal("引擎未收到 ctx")
	}
	// 编排层放弃等待后，引擎收到的 ctx 必须已被取消
	deadline := time.Now().Add(time.Second)
	for engineCtx.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if engineCtx.Err() == nil {
		t.Fatal("软超时后引擎收到的 ctx 应被取消（否则被放弃的调用会继续改状态）")
	}

	// 引擎观察到取消后不得推进共享状态
	time.Sleep(100 * time.Millisecond)
	if n := slow.mutations.Load(); n != 0 {
		t.Errorf("被放弃的调用不应推进引擎共享状态, mutations=%d", n)
	}
}

// TestHybridSearch_LegacyEngineStillProtected 未实现 ContextSearcher 的旧引擎
// 仍受软超时保护：编排层放弃等待并按 timeout 上报（B4 的旧接口兜底路径）。
func TestHybridSearch_LegacyEngineStillProtected(t *testing.T) {
	legacy := &slowMockEngine{name: "legacy", delay: 2 * time.Second}
	hs := NewHybridSearch(legacy)
	hs.SetEngineTimeout(40 * time.Millisecond)

	started := time.Now()
	_, diag, err := hs.SearchRawDiagnostics("q")
	if err == nil {
		t.Fatal("唯一引擎超时应返回错误")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("软超时未生效, 耗时 %v", elapsed)
	}
	if len(diag.Failures) != 1 || diag.Failures[0].Kind != core.FailureTimeout {
		t.Fatalf("应按 timeout 归类, got %+v", diag.Failures)
	}
}

// TestHybridSearch_ParallelLegacyEnginesNoRace 并发多引擎（含旧接口）下诊断收集无竞争。
func TestHybridSearch_ParallelLegacyEnginesNoRace(t *testing.T) {
	engines := make([]core.SearchInf, 0, 6)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		engines = append(engines, &slowMockEngine{name: name, delay: 5 * time.Millisecond,
			results: []core.SearchResult{{Title: name, Url: "https://example.com/" + name, Engine: name}}})
	}
	engines = append(engines, &slowMockEngine{name: "flaky", failOn: "boom", delay: 5 * time.Millisecond})

	hs := NewHybridSearch(engines...)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = hs.SearchRawDiagnostics("boom")
		}()
	}
	wg.Wait()
}
