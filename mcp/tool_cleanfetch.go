package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/fetch/jina"
	"websearch/pkg/fetch/sitefetch"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
	"websearch/pkg/telemetry"

	fetchengine "github.com/daidaiJ/go-webfetch"
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
	return cleanFetch(ctx, req, params, true)
}

func cleanFetch(ctx context.Context, req *mcp.CallToolRequest, params *CleanFetchParams, fetchEnabled bool) (result *mcp.CallToolResult, extra any, err error) {
	requestID := telemetry.NewRequestID()
	ctx = telemetry.WithRequestID(ctx, requestID)
	started := time.Now()
	count := 0
	query := params.URL
	mode := "url_fetch"
	if params.ResultRef != "" {
		query = ""
		mode = "saved_result"
	}
	defer func() {
		chain := ""
		if mode == "url_fetch" {
			chain = recentAttemptChain(started, 15)
		}
		telemetry.RecordEventContext(ctx, telemetry.Event{Kind: "tool", Tool: "cleanfetch", Query: query, Detail: mode, Success: err == nil, Duration: time.Since(started), ResultCount: count, Error: err, RequestID: requestID, AttemptChain: chain})
	}()
	if params.ResultRef != "" {
		if params.URL != "" || len(params.URLs) != 0 {
			return nil, nil, errors.New(i18n.T("tool.cleanfetch.ref_url_conflict"))
		}
		read := &ReadResultParams{Ref: params.ResultRef}
		if params.Offset != nil {
			read.Offset = *params.Offset
		}
		if params.Limit != nil {
			read.Limit = *params.Limit
		}
		_, page, err := readSavedResult(ctx, req, read)
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(page)
		if err != nil {
			return nil, nil, err
		}
		count = 1
		result := textResult(string(body))
		result.StructuredContent = page
		return result, nil, nil
	}
	if params.Offset != nil || params.Limit != nil {
		return nil, nil, errors.New(i18n.T("tool.cleanfetch.offset_requires_ref"))
	}
	if !fetchEnabled {
		return nil, nil, errors.New(i18n.T("tool.cleanfetch.url_fetch_disabled"))
	}
	urls := mergeFetchURLs(params.URL, params.URLs)
	count = len(urls)
	if len(urls) == 0 {
		return nil, nil, errors.New(i18n.T("tool.cleanfetch.url_required"))
	}
	if len(urls) > maxBatchFetchURLs {
		return nil, nil, errors.New(i18n.T("tool.cleanfetch.batch_too_large", "Max", maxBatchFetchURLs, "Count", len(urls)))
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
			sb.WriteString(i18n.T("tool.cleanfetch.item_failed", "Err", it.err))
			continue
		}
		okCount++
		sb.WriteString(it.text)
	}
	log.InfoT("log.mcp.batch_fetch_finished_succeeded", "okCount", okCount, "len", len(urls))
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

// fetchViaGithubAPI github.com 站点特判公共入口（cleanfetch 与 smartsearch 的
// fetch_top_n 共用）：匹配则走 REST API 抓取并记遥测。matched=false 表示 URL
// 不在特判范围；err 非 nil 表示 API 失败，调用方回落常规抓取链——Jina Reader
// 能渲染 JS 页面，仍有产出可能。内部含 fetcher 的配额冷却（403/429 快速失败）。
func fetchViaGithubAPI(ctx context.Context, rawURL string) (text string, matched bool, err error) {
	if siteFetchGitHub == nil || !sitefetch.MatchGitHub(rawURL) {
		return "", false, nil
	}
	started := time.Now()
	text, err = siteFetchGitHub.Fetch(ctx, rawURL)
	telemetry.RecordEventContext(ctx, telemetry.Event{
		Kind: "provider", Provider: "github_api", Query: rawURL,
		Success: err == nil, Duration: time.Since(started), ResultCount: 1, Error: err,
	})
	if err != nil {
		log.InfoT("log.mcp.github_site_specific_api_fetch_failed", "err", err)
	}
	return text, true, err
}

// fetchCleanPage 抓取单个 URL：SSRF 预检后按 mcp_hub.fetch_mode
// 选择远端抓取链（供应商 fetch API → MCP 托管抓取）与本地 webfetch/Jina 的
// 顺序，任一层失败自动降级。
func fetchCleanPage(ctx context.Context, rawURL string) (string, error) {
	if err := validateURLSecurity(rawURL); err != nil {
		return "", err
	}
	// 站点特判：HTML 抓不到的站点（github.com 为 JS 渲染）优先走官方 API，
	// 不做 headCheck、不进远端/本地降级链；API 失败（限流/403/路径不接管）时
	// 回落常规抓取链。
	if text, matched, apiErr := fetchViaGithubAPI(ctx, rawURL); matched && apiErr == nil {
		return text, nil
	}
	if err := headCheck(ctx, rawURL); err != nil {
		return "", err
	}
	local := func(ctx context.Context, u string) (string, error) {
		return fetchLocalPage(ctx, u)
	}
	// MCP 抓取面向 HTML/文本；PDF 继续走本地解析与 MinerU 链路。
	if isPDFURL(rawURL) {
		return local(ctx, rawURL)
	}
	return fetchPageWithFallback(ctx, rawURL, mcpHubFetchMode, remoteFetchChain(), local)
}

// remoteFetchChain 返回远端抓取链：供应商 fetch API（优先消耗按 URL
// 计数的免费额度）→ mcphub 托管 MCP。均未启用时返回 nil，走本地。
func remoteFetchChain() []mcpHubFetcher {
	chain := make([]mcpHubFetcher, 0, len(providerFetchers)+1)
	chain = append(chain, providerFetchers...)
	if mcpHubFetchInst != nil {
		chain = append(chain, mcpHubFetchInst)
	}
	if len(chain) == 0 {
		return nil
	}
	return chain
}

// fetchPageWithFallback 按 off/prefer/fallback 编排远端抓取链与本地抓取。
// 两条链路都失败时同时保留两侧错误，便于归因。
func fetchPageWithFallback(ctx context.Context, rawURL, mode string, remote []mcpHubFetcher,
	local func(context.Context, string) (string, error)) (string, error) {
	if len(remote) == 0 || mode == "" || mode == config.MCPHubFetchOff {
		return local(ctx, rawURL)
	}
	// 逐个尝试远端 fetcher：供应商 fetch API → MCP 托管；失败级联到下一个。
	tryRemote := func() (string, error) {
		var errs []string
		for _, f := range remote {
			text, err := f.Fetch(ctx, rawURL)
			if err == nil && strings.TrimSpace(text) != "" {
				return text, nil
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if err == nil {
				err = errors.New(i18n.T("tool.cleanfetch.empty_result"))
			}
			errs = append(errs, err.Error())
		}
		return "", errors.New(strings.Join(errs, i18n.T("tool.cleanfetch.error_separator")))
	}
	switch mode {
	case config.MCPHubFetchPrefer:
		text, err := tryRemote()
		if err == nil {
			return text, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.InfoT("log.mcp.remote_fetch_failed_degrading_to_local", "err", err)
		text, localErr := local(ctx, rawURL)
		if localErr == nil {
			return text, nil
		}
		return "", fmt.Errorf("%s: %w; %s: %w", i18n.T("tool.cleanfetch.remote_failed"), err, i18n.T("tool.cleanfetch.local_failed"), localErr)
	case config.MCPHubFetchFallback:
		text, localErr := local(ctx, rawURL)
		if localErr == nil {
			return text, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.InfoT("log.mcp.local_fetch_failed_trying_the_remote", "localErr", localErr)
		text, remoteErr := tryRemote()
		if remoteErr == nil {
			return text, nil
		}
		return "", fmt.Errorf("%s: %w; %s: %w", i18n.T("tool.cleanfetch.local_failed"), localErr, i18n.T("tool.cleanfetch.remote_failed"), remoteErr)
	default:
		return local(ctx, rawURL)
	}
}

func isPDFURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

// fetchLocalPage 现有本地链路：webfetch（无需代理）→ Jina Reader（需代理）。
func fetchLocalPage(ctx context.Context, rawURL string) (string, error) {
	started := time.Now()
	recordProvider := func(event telemetry.Event) {
		event.Kind = "provider"
		event.Query = rawURL
		telemetry.RecordEventContext(ctx, event)
	}

	// ── 第一层：go-webfetch（无需代理）──
	if webfetchInst != nil {
		result, err := webfetchInst.Fetch(ctx, rawURL)
		if err == nil {
			recordProvider(telemetry.Event{Provider: "webfetch", Success: true, Duration: time.Since(started), ResultCount: 1})
			return formatWebFetchResult(result), nil
		}
		recordProvider(telemetry.Event{Provider: "webfetch", Success: false, Duration: time.Since(started), Error: err})
		if !allowFetchFallback(err) {
			return "", err
		}
		log.InfoT("log.mcp.webfetch_fetch_failed_trying_fallback_to", "err", err)

		// ── 第二层：Jina Reader（需代理，jinaInst != nil 即表示代理已开启）──
		if jinaInst != nil {
			jinaResult, jinaErr := jinaInst.Fetch(rawURL)
			if jinaErr == nil {
				recordProvider(telemetry.Event{Provider: "jina", Success: true, Duration: time.Since(started), ResultCount: 1})
				return formatJinaResult(jinaResult), nil
			}
			recordProvider(telemetry.Event{Provider: "jina", Success: false, Duration: time.Since(started), Error: jinaErr})
			return "", fmt.Errorf("webfetch: %w; %s: %w", err, i18n.T("tool.cleanfetch.jina_fallback"), jinaErr)
		}
		return "", fmt.Errorf("%s: %w", i18n.T("tool.cleanfetch.webfetch_failed"), err)
	}

	// webfetch 未初始化，仅用 Jina（兼容旧模式：仅代理+Jina Key）
	if jinaInst != nil {
		jinaResult, jinaErr := jinaInst.Fetch(rawURL)
		if jinaErr != nil {
			recordProvider(telemetry.Event{Provider: "jina", Success: false, Duration: time.Since(started), Error: jinaErr})
			return "", fmt.Errorf("%s: %w", i18n.T("tool.cleanfetch.jina_failed"), jinaErr)
		}
		recordProvider(telemetry.Event{Provider: "jina", Success: true, Duration: time.Since(started), ResultCount: 1})
		return formatJinaResult(jinaResult), nil
	}

	return "", errors.New(i18n.T("tool.cleanfetch.no_fetcher_initialized"))
}

func allowFetchFallback(err error) bool {
	var missing *fetchengine.NotFoundError
	var blocked *fetchengine.SSRFError
	var challenge *fetchengine.WAFError
	return err != nil && !errors.As(err, &missing) && !errors.As(err, &blocked) &&
		!errors.As(err, &challenge) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
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
		fmt.Fprintf(&sb, "%s\n\n", i18n.T("tool.cleanfetch.published_time", "Time", result.PublishedTime))
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
		fmt.Fprintf(&sb, "%s\n\n", i18n.T("tool.cleanfetch.saved_to_file", "Lines", result.TotalLines, "Chars", result.TotalChars))
		fmt.Fprintf(&sb, "%s\n\n", i18n.T("tool.cleanfetch.file_path", "Path", result.FilePath))
		sb.WriteString(savedResultReadNotice(result))
	}
	return sb.String()
}
