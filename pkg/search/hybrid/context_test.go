package hybrid

import (
	"context"
	"testing"
	"time"
	"websearch/pkg/search/core"
)

// ctxAwareEngine 记录收到的 ctx 并支持延迟返回，用于验证 ctx 下传与软超时。
type ctxAwareEngine struct {
	name    string
	results []core.SearchResult
	delay   time.Duration
	gotCtx  chan context.Context
}

func (e *ctxAwareEngine) Name() string                        { return e.name }
func (e *ctxAwareEngine) Search(query string) (string, error) { return "", nil }
func (e *ctxAwareEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	time.Sleep(e.delay)
	return e.results, nil
}
func (e *ctxAwareEngine) MergeContent(query string, results []core.SearchResult) (string, error) {
	return "", nil
}
func (e *ctxAwareEngine) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	if e.gotCtx != nil {
		select {
		case e.gotCtx <- ctx:
		default:
		}
	}
	select {
	case <-time.After(e.delay):
		return e.results, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestHybridSearch_EngineSoftTimeout 慢引擎超过 per-engine 软超时后放弃等待，
// 按 timeout 进失败清单；其余引擎结果照常返回。
func TestHybridSearch_EngineSoftTimeout(t *testing.T) {
	fast := &ctxAwareEngine{name: "bing", results: []core.SearchResult{
		{Title: "ok", Url: "https://example.com/ok", Engine: "bing"},
	}}
	slow := &ctxAwareEngine{name: "slow", delay: 2 * time.Second}
	hs := NewHybridSearch(fast, slow)
	hs.SetEngineTimeout(50 * time.Millisecond)

	started := time.Now()
	results, diag, err := hs.SearchRawDiagnostics("test")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("软超时未生效，耗时 %v", elapsed)
	}
	if len(results) != 1 || results[0].Title != "ok" {
		t.Fatalf("快引擎结果应正常返回，实际 %+v", results)
	}
	if !diag.HasFailures() || diag.Failures[0].Engine != "slow" || diag.Failures[0].Kind != core.FailureTimeout {
		t.Fatalf("慢引擎应按 timeout 进失败清单，实际 %+v", diag.Failures)
	}
}

// TestHybridSearch_RequestContextCancelled 请求 ctx 取消后立即返回，
// 引擎按 timeout 类型进失败清单（与软超时同一口径）。
func TestHybridSearch_RequestContextCancelled(t *testing.T) {
	slow := &ctxAwareEngine{name: "slow", delay: 5 * time.Second}
	hs := NewHybridSearch(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, diag, err := hs.SearchRawContextDiagnostics(ctx, "test")
	if err == nil {
		t.Fatal("全部引擎被取消时应返回错误")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("请求取消未传播，耗时 %v", elapsed)
	}
	if !diag.HasFailures() || diag.Failures[0].Kind != core.FailureTimeout {
		t.Fatalf("取消应按 timeout 归类，实际 %+v", diag.Failures)
	}
}

// TestHybridSearch_ContextPropagates 请求 ctx 原样下传实现 ContextSearcher 的引擎。
func TestHybridSearch_ContextPropagates(t *testing.T) {
	got := make(chan context.Context, 1)
	engine := &ctxAwareEngine{name: "ctx-engine", gotCtx: got, results: []core.SearchResult{
		{Title: "r", Url: "https://example.com/r"},
	}}
	hs := NewHybridSearch(engine)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	if _, _, err := hs.SearchRawContextDiagnostics(ctx, "test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case received := <-got:
		if received.Value(ctxKey{}) != "marker" {
			t.Error("引擎收到的 ctx 未携带请求值，ctx 未贯通")
		}
	default:
		t.Fatal("引擎未收到 ctx")
	}
}

// TestHybridSearch_LegacyEngineTimeoutFallback 未实现 ContextSearcher 的旧引擎
// 同样受软超时保护：放弃等待并按 timeout 上报。
func TestHybridSearch_LegacyEngineTimeoutFallback(t *testing.T) {
	legacySlow := &slowMockEngine{name: "legacy", delay: 2 * time.Second,
		results: []core.SearchResult{{Title: "late", Url: "https://example.com/late"}}}
	hs := NewHybridSearch(legacySlow)
	hs.SetEngineTimeout(40 * time.Millisecond)

	started := time.Now()
	_, diag, err := hs.SearchRawDiagnostics("test")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("旧引擎软超时未生效，耗时 %v", elapsed)
	}
	if err == nil {
		t.Fatal("唯一引擎超时应返回错误")
	}
	if len(diag.Failures) != 1 || diag.Failures[0].Kind != core.FailureTimeout {
		t.Fatalf("应按 timeout 归类，实际 %+v", diag.Failures)
	}
}
