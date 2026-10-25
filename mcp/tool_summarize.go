package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"
	"websearch/pkg/llm"
	"websearch/pkg/log"
	"websearch/pkg/search"
)

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。

// LLM 摘要：流式生成与 MCP progress 推送。
// 客户端断开（ctx 取消）时自动中止。
func streamSummarize(ctx context.Context, req *mcp.CallToolRequest, query, intent string, results []search.SearchResult) (string, error) {
	notifyProgress(ctx, req, 1, 2, fmt.Sprintf("搜索完成，共 %d 条结果，正在生成摘要...", len(results)))

	ch := make(chan string, 64)
	errCh := make(chan error, 1)
	go summarizerInst.SummarizeStream(ctx, query, intent, results, ch, errCh)

	var sb strings.Builder
	progress := newProgressBatcher(ctx, req)
	for {
		select {
		case token, ok := <-ch:
			if !ok {
				// 完成契约（pkg/llm.ChatStream）：先关 ch 后发 errCh，
				// token 排干后阻塞读取必然拿到最终结果（含错误）。
				progress.flush()
				return llm.FormatCitation(query, sb.String(), results), <-errCh
			}
			sb.WriteString(token)
			progress.token(token)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// progressBatcher 累积摘要 token 文本，按可配置的最小间隔批量推送进度通知
// （llm.summary_progress_interval，默认 1s）。实测每 token 一次 MCP 通知
// 耗时 ~270-450µs（mock LLM 基准，BenchmarkStreamSummarizeProgressPerToken），
// 快速供应商下成为瓶颈；攒批后通知频率封顶 1/interval。
type progressBatcher struct {
	ctx      context.Context
	req      *mcp.CallToolRequest
	interval time.Duration
	sb       strings.Builder
	last     time.Time
}

// summaryProgressInterval 进度推送最小间隔，启动时由 applySummarizer 注入。
var summaryProgressInterval = time.Second

func newProgressBatcher(ctx context.Context, req *mcp.CallToolRequest) *progressBatcher {
	return &progressBatcher{ctx: ctx, req: req, interval: summaryProgressInterval}
}

// token 记录一个摘要 token，距上次推送超过 interval 时批量推送。
func (p *progressBatcher) token(tok string) {
	p.sb.WriteString(tok)
	if time.Since(p.last) >= p.interval {
		p.flush()
	}
}

// flush 推送累积文本并重置计时；空批无副作用。
func (p *progressBatcher) flush() {
	if p.sb.Len() == 0 {
		return
	}
	notifyProgress(p.ctx, p.req, 2, 2, p.sb.String())
	p.last = time.Now()
	p.sb.Reset()
}

// notifyProgress 向 MCP 客户端推送进度通知（StreamableHTTP 传输下实时到达）。
// 推送失败仅记录日志，不影响主流程。
func notifyProgress(ctx context.Context, req *mcp.CallToolRequest, progress, total float64, message string) {
	if req == nil || req.Session == nil || req.Params == nil || req.Params.GetProgressToken() == nil {
		return
	}
	params := &mcp.ProgressNotificationParams{
		ProgressToken: req.Params.GetProgressToken(),
		Progress:      progress,
		Total:         total,
		Message:       message,
	}
	if err := req.Session.NotifyProgress(ctx, params); err != nil {
		log.DebugT("log.mcp.progress_notification_push_failed", "err", err)
	}
}
