package hybrid

import (
	"errors"
	"testing"

	"websearch/pkg/antirobot"
	"websearch/pkg/search/core"
)

// localLimitedEngine 模拟「本端限流」的真实引擎返回：空结果 + 可归因的哨兵错误。
type localLimitedEngine struct{ name string }

func (e *localLimitedEngine) Name() string                        { return e.name }
func (e *localLimitedEngine) Search(query string) (string, error) { return "", nil }
func (e *localLimitedEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	resp, err := antirobot.LocalLimitSkipped(e.name)
	_ = resp
	return nil, err
}
func (e *localLimitedEngine) MergeContent(_ string, _ []core.SearchResult) (string, error) {
	return "", nil
}

// TestHybridSearch_LocalRateLimitIsAttributed 本端限流的引擎必须进失败清单并归到
// rate_limit（B12）。旧实现返回「空结果 + nil error」，失败清单为空、聚合错误变成
// 「所有引擎均未返回有效结果」，限流这一类在归因链路里完全消失。
func TestHybridSearch_LocalRateLimitIsAttributed(t *testing.T) {
	limited := &localLimitedEngine{name: "bing"}
	hs := NewHybridSearch(limited)

	_, diag, err := hs.SearchRawDiagnostics("q")
	if err == nil {
		t.Fatal("唯一引擎被本端限流时应返回错误")
	}
	if len(diag.Failures) != 1 {
		t.Fatalf("本端限流应进失败清单, got %+v", diag.Failures)
	}
	if diag.Failures[0].Kind != core.FailureRateLimit {
		t.Errorf("应归类为 rate_limit, got %q（旧实现无失败清单）", diag.Failures[0].Kind)
	}
}

// TestClassifyFailureSeesLocalRateLimitSentinel 判类函数必须能从哨兵识别限流，
// 且不依赖本地化文本（错误文案随语言切换不影响判类）。
func TestClassifyFailureSeesLocalRateLimitSentinel(t *testing.T) {
	wrapped := errors.New("bing: " + antirobot.ErrLocalRateLimit.Error())
	if got := core.ClassifyFailure(wrapped); got != core.FailureRateLimit {
		t.Errorf("哨兵应归类为 rate_limit, got %q", got)
	}
	// 引擎实际返回的错误（含本地化原因）同样必须判出限流
	_, err := antirobot.LocalLimitSkipped("bing")
	if got := core.ClassifyFailure(err); got != core.FailureRateLimit {
		t.Errorf("引擎返回错误应归类为 rate_limit, got %q", got)
	}
}
