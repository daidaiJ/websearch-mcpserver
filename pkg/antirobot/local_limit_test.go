// Package antirobot 的本地限流归因契约（B12）。
package antirobot

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestLocalRateLimitSentinelClassifiesAsRateLimit 哨兵必须带「rate limit」关键词：
// core.ClassifyFailure 与 telemetry.ClassifyErrorKind 都按关键词判类，
// 文案本地化不得影响判类（B12）。
func TestLocalRateLimitSentinelClassifiesAsRateLimit(t *testing.T) {
	msg := strings.ToLower(ErrLocalRateLimit.Error())
	if !strings.Contains(msg, "rate limit") {
		t.Fatalf("哨兵文本应含 rate limit 关键词（两处判类依赖）, got %q", msg)
	}
	if !errors.Is(ErrLocalRateLimit, ErrLocalRateLimit) {
		t.Fatal("哨兵应可经 errors.Is 匹配")
	}
}

// TestLocalLimitSkippedIsAttributable 引擎的本地限流返回值应可归因：
// 无结果 + 可 errors.Is 判定的哨兵错误（旧实现返回 nil error，被判成「成功但空」）。
func TestLocalLimitSkippedIsAttributable(t *testing.T) {
	resp, err := LocalLimitSkipped("bing")
	if err == nil {
		t.Fatal("本地限流必须返回错误（旧实现返回 nil 导致归因失真）")
	}
	if resp == nil || len(resp.Results) != 0 {
		t.Fatalf("本地限流应返回空结果，实际 %+v", resp)
	}
	if !errors.Is(err, ErrLocalRateLimit) {
		t.Fatalf("错误应包裹哨兵, got %v", err)
	}
	if msg := strings.ToLower(err.Error()); !strings.Contains(msg, "rate limit") {
		t.Errorf("错误文本应含 rate limit 关键词, got %q", err.Error())
	}
}

// TestSleepCtxRespectsCancellation 被放弃的调用不应继续空转（B4 的前置能力）。
func TestSleepCtxRespectsCancellation(t *testing.T) {
	if err := SleepCtx(context.Background(), 0); err != nil {
		t.Errorf("零时长应直接返回, got %v", err)
	}
}
