package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"websearch/pkg/fetch/jina"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/log"
	"websearch/pkg/telemetry"
)

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。

// cleanfetch 工具：单 URL / 批量抓取与结果格式化。
// ── CleanFetch 工具 ──────────────────────────────────────────────────────────

// CleanFetch 通过 go-webfetch 抓取网页，失败时回退到 Jina Reader。
// 支持 url + urls 批量（合并去重，最多 5 个）：并发抓取，单条失败不影响其它。
// 只传一个 URL 时输出与旧版完全一致。
func CleanFetch(ctx context.Context, req *mcp.CallToolRequest, params *CleanFetchParams) (result *mcp.CallToolResult, extra any, err error) {
	requestID := telemetry.NewRequestID()
	ctx = telemetry.WithRequestID(ctx, requestID)
	started := time.Now()
	count := 0
	defer func() {
		telemetry.RecordEventContext(ctx, telemetry.Event{Kind: "tool", Tool: "cleanfetch", Query: params.URL, Success: err == nil, Duration: time.Since(started), ResultCount: count, Error: err, RequestID: requestID, AttemptChain: recentAttemptChain(started, 15)})
	}()
	urls := mergeFetchURLs(params.URL, params.URLs)
	count = len(urls)
	if len(urls) == 0 {
		return nil, nil, fmt.Errorf("url 和 urls 参数至少填一个")
	}
	if len(urls) > maxBatchFetchURLs {
		return nil, nil, fmt.Errorf("批量抓取最多 %d 个 URL（当前 %d 个），请拆分多次调用", maxBatchFetchURLs, len(urls))
	}

	if len(urls) == 1 {
		text, err := fetchCleanPage(ctx, urls[0])
		if err != nil {
			return nil, nil, err
		}
		return textResult(text), nil, nil
	}

	type batchItem struct {
		text string
		err  error
	}
	items := make([]batchItem, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			text, err := fetchCleanPage(ctx, u)
			items[i] = batchItem{text: text, err: err}
		}(i, u)
	}
	wg.Wait()

	var sb strings.Builder
	okCount := 0
	for i, it := range items {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&sb, "## [%d] %s\n\n", i+1, urls[i])
		if it.err != nil {
			fmt.Fprintf(&sb, "**抓取失败**: %v", it.err)
			continue
		}
		okCount++
		sb.WriteString(it.text)
	}
	log.Infof("批量抓取完成: %d/%d 成功", okCount, len(urls))
	return textResult(sb.String()), nil, nil
}

// mergeFetchURLs 合并 url 与 urls：trim、去重、保持顺序。
func mergeFetchURLs(url string, urls []string) []string {
	seen := make(map[string]bool, len(urls)+1)
	out := make([]string, 0, len(urls)+1)
	for _, u := range append([]string{url}, urls...) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// fetchCleanPage 抓取单个 URL：SSRF 预检 + HEAD 预检 + webfetch（Jina 兜底），
// 返回格式化后的 Markdown 文本。
func fetchCleanPage(ctx context.Context, rawURL string) (string, error) {
	started := time.Now()
	recordProvider := func(event telemetry.Event) {
		event.Kind = "provider"
		event.Query = rawURL
		telemetry.RecordEventContext(ctx, event)
	}
	// ── 安全预检：DNS rebinding 防护 ──
	if err := validateURLSecurity(rawURL); err != nil {
		return "", err
	}

	// ── HEAD 预检：检测文件大小和类型 ──
	if err := headCheck(ctx, rawURL); err != nil {
		return "", err
	}

	// ── 第一层：go-webfetch（无需代理）──
	if webfetchInst != nil {
		result, err := webfetchInst.Fetch(ctx, rawURL)
		if err == nil {
			recordProvider(telemetry.Event{Provider: "webfetch", Success: true, Duration: time.Since(started), ResultCount: 1})
			return formatWebFetchResult(result), nil
		}
		recordProvider(telemetry.Event{Provider: "webfetch", Success: false, Duration: time.Since(started), Error: err})
		log.Infof("webfetch 抓取失败(%v)，尝试回退到 Jina Reader", err)

		// ── 第二层：Jina Reader（需代理，jinaInst != nil 即表示代理已开启）──
		if jinaInst != nil {
			jinaResult, jinaErr := jinaInst.Fetch(rawURL)
			if jinaErr == nil {
				recordProvider(telemetry.Event{Provider: "jina", Success: true, Duration: time.Since(started), ResultCount: 1})
				return formatJinaResult(jinaResult), nil
			}
			recordProvider(telemetry.Event{Provider: "jina", Success: false, Duration: time.Since(started), Error: jinaErr})
			return "", fmt.Errorf("webfetch: %v; Jina 兜底: %w", err, jinaErr)
		}
		return "", fmt.Errorf("webfetch 抓取失败: %v", err)
	}

	// webfetch 未初始化，仅用 Jina（兼容旧模式：仅代理+Jina Key）
	if jinaInst != nil {
		jinaResult, jinaErr := jinaInst.Fetch(rawURL)
		if jinaErr != nil {
			recordProvider(telemetry.Event{Provider: "jina", Success: false, Duration: time.Since(started), Error: jinaErr})
			return "", fmt.Errorf("jina reader 抓取失败: %w", jinaErr)
		}
		recordProvider(telemetry.Event{Provider: "jina", Success: true, Duration: time.Since(started), ResultCount: 1})
		return formatJinaResult(jinaResult), nil
	}

	return "", fmt.Errorf("webfetch 和 jina reader 均未初始化")
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// resolvePDFPath 将 pdf_parser 的 path 规范为 webfetch 可消费的 URL。

// formatJinaResult 将 Jina Reader 结果格式化为 Markdown 文本。
func formatJinaResult(result *jina.FetchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", result.Title)
	if result.Description != "" {
		fmt.Fprintf(&sb, "> %s\n\n", result.Description)
	}
	if result.PublishedTime != "" {
		fmt.Fprintf(&sb, "**发布时间**: %s\n\n", result.PublishedTime)
	}
	sb.WriteString(result.Content)
	return sb.String()
}

// formatWebFetchResult 将 go-webfetch 结果格式化为 Markdown 文本。
func formatWebFetchResult(result *webfetch.Result) string {
	var sb strings.Builder
	if result.Preamble != "" {
		fmt.Fprintf(&sb, "%s\n\n", result.Preamble)
	}
	if result.Mode == "inline" {
		if result.Title != "" {
			fmt.Fprintf(&sb, "# %s\n\n", result.Title)
		}
		sb.WriteString(result.Markdown)
	} else {
		// 大文本已存储到文件
		if result.Title != "" {
			fmt.Fprintf(&sb, "# %s\n\n", result.Title)
		}
		fmt.Fprintf(&sb, "内容已保存到文件（共 %d 行，%d 字符）\n\n", result.TotalLines, result.TotalChars)
		fmt.Fprintf(&sb, "**文件路径**: `%s`\n\n", result.FilePath)
		if result.AgentHint != "" {
			fmt.Fprintf(&sb, "**读取提示**: %s\n", result.AgentHint)
		}
	}
	return sb.String()
}
