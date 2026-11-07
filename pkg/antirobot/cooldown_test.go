package antirobot

import (
	"context"
	"testing"
	"time"
)

// ctxSpyEngine 记录收到的 ctx 与推进的共享状态次数。
type ctxSpyEngine struct {
	name      string
	gotCtx    chan context.Context
	mutations int
}

func (e *ctxSpyEngine) Name() string          { return e.name }
func (e *ctxSpyEngine) Region() NetworkRegion { return RegionChina }
func (e *ctxSpyEngine) Search(string, int, TimeRange) (*SearchResponse, error) {
	e.mutations++ // 旧接口路径：被放弃后仍会推进状态
	return nil, context.DeadlineExceeded
}
func (e *ctxSpyEngine) SearchContext(ctx context.Context, _ string, _ int, _ TimeRange) (*SearchResponse, error) {
	if e.gotCtx != nil {
		select {
		case e.gotCtx <- ctx:
		default:
		}
	}
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	e.mutations++
	return nil, context.DeadlineExceeded
}

// TestSearcherPassesContextToContextEngine 编排层优先走 ContextEngine 入口（B4）：
// 软超时后引擎收到的 ctx 必须已被取消，引擎据此停止在飞工作。
func TestSearcherPassesContextToContextEngine(t *testing.T) {
	got := make(chan context.Context, 1)
	eng := &ctxSpyEngine{name: "spy", gotCtx: got}
	s := NewSearcher(StrategyParallel, []Engine{eng})
	s.PerEngineTimeout = 50 * time.Millisecond

	started := time.Now()
	responses := s.Search(context.Background(), "q", 1)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("软超时未生效, 耗时 %v", elapsed)
	}
	if len(responses) != 1 || responses[0].Error == "" {
		t.Fatalf("超时应产生错误响应, got %+v", responses)
	}

	select {
	case engineCtx := <-got:
		deadline := time.Now().Add(time.Second)
		for engineCtx.Err() == nil && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if engineCtx.Err() == nil {
			t.Fatal("软超时后引擎收到的 ctx 应被取消")
		}
	default:
		t.Fatal("实现 ContextEngine 的引擎应走 ctx 入口")
	}

	time.Sleep(100 * time.Millisecond)
	if eng.mutations != 0 {
		t.Errorf("被放弃的调用不应推进引擎状态, mutations=%d", eng.mutations)
	}
}

// TestNextCooldownDuration 冷却档位升级规则（各引擎共用的唯一实现）。
func TestNextCooldownDuration(t *testing.T) {
	base, max := 20*time.Second, 2*time.Minute
	tests := []struct {
		name       string
		current    time.Duration
		retryAfter time.Duration
		want       time.Duration
	}{
		{"首次取 base", 0, 0, base},
		{"同档翻倍", base, 0, 2 * base},
		{"翻倍封顶", max, 0, max},
		{"Retry-After 优先", 0, 30 * time.Second, 30 * time.Second},
		{"Retry-After 也封顶", 0, time.Hour, max},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextCooldownDuration(tc.current, base, max, tc.retryAfter); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
