package antirobot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"websearch/pkg/i18n"
)

// ErrLocalRateLimit 是本端限流器拒绝放行的稳定哨兵错误。
//
// 各引擎此前对 `limiter.Allow() == false` 的处置不一致：ddg / arxiv 返回 error
// （失败清单能归到 rate_limit），而 baidu / bing / google / github / googlenews /
// wikipedia / so360 返回「空结果 + nil error」——编排层把它记成「成功但零结果」，
// ClassifyFailure 与 telemetry.ClassifyErrorKind 都看不到限流这一类（B12）。
// 统一返回本哨兵后，归因链路（失败清单 / 控制中心失败构成）才完整。
//
// 哨兵自身文本固定为英文（不做本地化）：它是 errors.Is 的身份，同时「rate limit」
// 也落在 core.ClassifyFailure / telemetry.ClassifyErrorKind 的关键词表内。
var ErrLocalRateLimit = errors.New("local rate limit")

// LocalRateLimitReason 本端限流的短原因（本地化，进失败清单与日志）。
func LocalRateLimitReason() string {
	return i18n.T("search.engine.local_rate_limited")
}

// SleepCtx 按 ctx 可取消地等待 d；d 非正时只回报 ctx 状态。
// 编排层软超时（B4）后，被放弃的调用不应继续空转。
func SleepCtx(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LocalLimitSkipped 本端限流跳过本次请求时的引擎返回值：无结果且带哨兵错误，
// 让编排层把它记进失败清单（kind=rate_limit）而不是「成功但空」。
// 错误文本用本地化短原因 + 不可本地化的哨兵，兼顾可读性与稳定判类。
func LocalLimitSkipped(engine string) (*SearchResponse, error) {
	return &SearchResponse{Engine: engine, Results: []Result{}},
		fmt.Errorf("%s: %s: %w", engine, LocalRateLimitReason(), ErrLocalRateLimit)
}
