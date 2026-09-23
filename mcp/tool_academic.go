package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"
)

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。

// academicsearch 工具：handler、学术搜索与结果合并。
// AcademicSearchHandler 学术搜索 tool handler。
func AcademicSearchHandler(ctx context.Context, req *mcp.CallToolRequest, params *AcademicSearchParams) (*mcp.CallToolResult, any, error) {
	return doAcademicSearch(ctx, params.Query, params.Engines, params.TimeRange, params.Page)
}

// doWebSearch 通用网页搜索逻辑。
// timeRangeMonths 控制搜索时间范围（月），默认 3，0 表示不限。

// doAcademicSearch 学术搜索逻辑。
func doAcademicSearch(ctx context.Context, query string, engines []string, timeRange string, page int) (result *mcp.CallToolResult, extra any, err error) {
	started := time.Now()
	requestID := telemetry.RequestID(ctx)
	if requestID == "" {
		requestID = telemetry.NewRequestID()
		ctx = telemetry.WithRequestID(ctx, requestID)
	}
	cacheHit := false
	resultCount := 0
	defer func() {
		telemetry.RecordEventContext(ctx, telemetry.Event{Kind: "tool", Tool: "academicsearch", Query: query, Success: err == nil, Duration: time.Since(started), CacheHit: cacheHit, ResultCount: resultCount, Error: err, RequestID: requestID, AttemptChain: recentAttemptChain(started, 20)})
	}()
	if academicSearcher == nil {
		return nil, nil, fmt.Errorf("学术搜索引擎未启用，请检查配置 bing.academic 是否为 true")
	}

	// ---- 缓存查询 ----
	enginesKey := strings.Join(engines, ",")
	cacheKey := query + "|" + timeRange + "|" + enginesKey
	if cacheInst != nil {
		rec, hitType, err := cacheInst.Lookup(cacheKey, "", true)
		if err != nil {
			log.Errf("缓存查询异常，跳过缓存: %v", err)
		} else if rec != nil && rec.Academic && hitType == "query_only" {
			results, parseErr := rec.GetRawResults()
			if parseErr == nil {
				cacheHit = true
				resultCount = len(results)
				recordAcademicProviderEvents(query, results, nil, nil, started, requestID)
				log.Infof("学术缓存命中: query=%s", query)
				ret, mergeErr := formatAcademicResults(query, search.AcademicSearchResult{Results: results})
				if mergeErr == nil {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
				}
			}
		}
	}

	// ---- 学术搜索 ----
	opts := search.AcademicSearchOptions{
		Page:      page,
		TimeRange: timeRange,
		Engines:   engines,
	}

	log.Infof("学术搜索: query=%s, engines=%v, timeRange=%s, page=%d", query, engines, timeRange, page)
	res, err := academicSearcher.SearchAcademicRaw(query, opts)
	if err != nil {
		// Provider failures must still be visible in telemetry even though the
		// aggregate tool call returned an error.
		recordAcademicProviderEvents(query, nil, academicProviderErrorsFromError(err), academicEngineSelection(academicSearcher.AcademicEngines(), engines), started, requestID)
		return nil, nil, fmt.Errorf("学术搜索失败: %w", err)
	}
	resultCount = len(res.Results)
	recordAcademicProviderEvents(query, res.Results, res.EngineErrors, academicEngineSelection(academicSearcher.AcademicEngines(), engines), started, requestID)

	ret, err := formatAcademicResults(query, res)
	if err != nil {
		return nil, nil, err
	}
	if cacheInst != nil {
		// 逐引擎错误不写入缓存（Store 仍只存干净结果），命中路径展示的是上次结果
		_ = cacheInst.Store(cacheKey, "", true, res.Results, "")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

// academicProviderErrorsFromError extracts the adapter's "engine: message"
// summary so a fully failed aggregate call still produces provider events.
func academicProviderErrorsFromError(err error) map[string]string {
	if err == nil {
		return nil
	}
	const prefix = "学术引擎搜索无结果（"
	msg := err.Error()
	start := strings.Index(msg, prefix)
	if start < 0 {
		return nil
	}
	body := msg[start+len(prefix):]
	body = strings.TrimSuffix(body, "）")
	out := make(map[string]string)
	for _, part := range strings.Split(body, "; ") {
		name, detail, ok := strings.Cut(part, ": ")
		if ok && name != "" {
			out[name] = detail
		}
	}
	return out
}

// academicEngineSelection returns only the engines that actually ran. An empty
// requested list means the adapter searched every registered engine.
func academicEngineSelection(registered, requested []string) []string {
	if len(registered) == 0 {
		return requested
	}
	if len(requested) == 0 {
		return registered
	}
	selected := make([]string, 0, len(requested))
	for _, name := range requested {
		for _, available := range registered {
			if strings.EqualFold(strings.TrimSpace(name), available) {
				selected = append(selected, available)
				break
			}
		}
	}
	return selected
}

// recordAcademicProviderEvents attributes each merged academic result to every
// engine that returned it. Empty successes and per-engine failures still emit
// one event so the dashboard shows coverage for every provider, not just the
// engines that happened to contribute the final result set.
func recordAcademicProviderEvents(query string, results []search.SearchResult, engineErrors map[string]string, engines []string, started time.Time, requestID string) {
	counts := make(map[string]int)
	failed := make(map[string]bool)
	engineTrace := make([]string, 0, len(engines))
	seenEngine := make(map[string]bool)

	mark := func(name string) {
		if name == "" || seenEngine[name] {
			return
		}
		seenEngine[name] = true
		engineTrace = append(engineTrace, name)
	}
	for _, name := range engines {
		mark(name)
	}
	for _, item := range results {
		names := item.Engines
		if len(names) == 0 && item.Engine != "" {
			names = []string{item.Engine}
		}
		for _, name := range names {
			if name != "" {
				counts[name]++
				mark(name)
			}
		}
	}
	for name := range engineErrors {
		failed[name] = true
		mark(name)
	}

	// The adapter exposes per-engine errors but not per-engine timing yet, so
	// each provider event carries the aggregate call duration.
	duration := time.Since(started)
	for _, name := range engineTrace {
		event := telemetry.Event{
			Kind:        "provider",
			Provider:    name,
			Query:       query,
			Success:     !failed[name],
			Duration:    duration,
			ResultCount: counts[name],
			RequestID:   requestID,
			AttemptChain: func() string {
				if !failed[name] {
					return name
				}
				return ""
			}(),
		}
		if failed[name] {
			event.Error = fmt.Errorf("%s", engineErrors[name])
		}
		telemetry.Record(event)
	}
}

// streamSummarize 流式生成摘要：通过 MCP progress notification 逐 token 推送，
// 同时累积全文，流结束后返回完整格式化摘要（含引用）。

func formatAcademicResults(query string, res search.AcademicSearchResult) (string, error) {
	if adapter, ok := academicSearcher.(*search.AcademicAdapter); ok {
		return adapter.MergeContentWithErrors(query, res.Results, res.EngineErrors)
	}
	return searchapi.MergeContent(query, res.Results)
}

func formatRawResults(query string, results []search.SearchResult) (string, error) {
	return searchapi.MergeContent(query, results)
}
