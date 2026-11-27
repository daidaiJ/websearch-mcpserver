package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	academicpkg "websearch/pkg/academic"
	"websearch/pkg/cache"
	"websearch/pkg/i18n"
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
	return doAcademicSearchWithRequest(ctx, req, params.Query, params.Engines, params.TimeRange, params.Page)
}

// doWebSearch 通用网页搜索逻辑。
// timeRangeMonths 控制搜索时间范围（月），默认 3，0 表示不限。

// doAcademicSearch 学术搜索逻辑。
func doAcademicSearch(ctx context.Context, query string, engines []string, timeRange string, page int) (result *mcp.CallToolResult, extra any, err error) {
	return doAcademicSearchWithRequest(ctx, nil, query, engines, timeRange, page)
}

// doAcademicSearchWithRequest 携带原始工具请求，用于按 MCP 会话压缩
// 重复的 provenance 提示；测试与内部调用可传 nil。
func doAcademicSearchWithRequest(ctx context.Context, req *mcp.CallToolRequest, query string, engines []string, timeRange string, page int) (result *mcp.CallToolResult, extra any, err error) {
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
		return nil, nil, errors.New(i18n.T("tool.academic.disabled"))
	}
	if page <= 0 {
		page = 1
	}

	// ---- 缓存查询 ----
	// page 必须进键（B7）：否则 page>=2 会命中第一页的缓存记录，分页静默退化。
	// Lookup 与 Store 必须共用同一个键构造函数。省略 / 非正页码与适配器
	// 的 clamp（<=0 → 1）对齐，避免 MCP 缺省 page=0 与真实检索 page=1 各写各的键。
	cacheKey := academicCacheKey(query, timeRange, engines, page)
	requestCache := requestSearchCache()
	if requestCache != nil {
		rec, hitType, err := requestCache.Lookup(cacheKey, "", true)
		if err != nil {
			log.ErrorT("log.mcp.cache_query_error_skipping_cache", "err", err)
		} else if rec != nil && rec.Academic && hitType == "query_only" {
			results, parseErr := rec.GetRawResults()
			if parseErr == nil {
				cacheHit = true
				resultCount = len(results)
				log.InfoT("log.mcp.academic_cache_hit_query", "query", query)
				meta := decodeAcademicCacheMeta(rec.Meta)
				if len(meta.EngineErrors) != 0 {
					// 部分成功的缓存只复用成功引擎；失败引擎按引擎重试，
					// 避免九引擎全量重跑，同时保持「下次调用会补查」。
					merged := retryPartialAcademic(ctx, query, search.AcademicSearchOptions{
						Page: page, TimeRange: timeRange, Engines: engines,
					}, results, meta)
					storeAcademicCache(requestCache, cacheKey, timeRange, merged)
					results, resultCount = merged.Results, len(merged.Results)
					meta.EngineErrors = merged.EngineErrors
				}
				ret, mergeErr := formatAcademicResults(query, search.AcademicSearchResult{Results: results, EngineErrors: meta.EngineErrors})
				if mergeErr == nil {
					// 缓存命中：retrieved_at 为原检索时间，附带 cache_age_seconds
					ret = provenanceHeaderForRequest(req, rec.CreatedAt, time.Since(rec.CreatedAt)) + ret
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

	log.InfoT("log.mcp.academic_search_query_engines_timerange_page", "query", query, "engines", engines, "timeRange", timeRange, "page", page)
	res, err := callAcademicSearcher(ctx, query, opts)
	attempted := res.AttemptedEngines
	attemptsKnown := attempted != nil
	if !attemptsKnown {
		attempted = academicEngineSelection(academicSearcher.AcademicEngines(), engines)
	}
	if err != nil {
		// Zero hits across healthy engines is an empty result, not a provider
		// failure; return it as a successful call so callers can distinguish
		// "nothing matched" from "the engines failed".
		kind, _ := academicpkg.ParsePaperQuery(query)
		if kind == "" && len(res.EngineErrors) == 0 && err.Error() == i18n.T("search.academic.no_results") {
			recordAcademicProviderEvents(ctx, query, nil, nil, attempted, attemptsKnown, started, requestID)
			text := i18n.T("search.academic.no_results")
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		}
		// Provider failures must still be visible in telemetry even though the
		// aggregate tool call returned an error.
		engineErrors := res.EngineErrors
		if len(engineErrors) == 0 {
			engineErrors = academicProviderErrorsFromError(err)
		}
		if attemptsKnown && len(engineErrors) == 0 {
			engineErrors = make(map[string]string, len(attempted))
			for _, name := range attempted {
				engineErrors[name] = i18n.T("tool.academic.exact_no_result")
			}
		}
		recordAcademicProviderEvents(ctx, query, nil, engineErrors, attempted, attemptsKnown, started, requestID)
		return nil, nil, fmt.Errorf("%s: %w", i18n.T("tool.academic.search_failed"), err)
	}
	resultCount = len(res.Results)
	recordAcademicProviderEvents(ctx, query, res.Results, res.EngineErrors, attempted, attemptsKnown, started, requestID)

	ret, err := formatAcademicResults(query, res)
	if err != nil {
		return nil, nil, err
	}
	storeAcademicCache(requestCache, cacheKey, timeRange, res)
	ret = provenanceHeaderForRequest(req, time.Now(), 0) + ret
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

// academicCacheMeta 随部分成功的结果一起缓存逐引擎失败，命中时仍能显示
// 「结果可能不完整」，而不是把部分结果伪装成完整结果。
type academicCacheMeta struct {
	EngineErrors map[string]string `json:"engine_errors,omitempty"`
}

const partialAcademicTTL = 10 * time.Minute

func decodeAcademicCacheMeta(raw string) academicCacheMeta {
	var meta academicCacheMeta
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &meta)
	}
	return meta
}

func callAcademicSearcher(ctx context.Context, query string, opts search.AcademicSearchOptions) (search.AcademicSearchResult, error) {
	if ctxSearcher, ok := academicSearcher.(search.ContextAcademicSearcher); ok {
		return ctxSearcher.SearchAcademicRawContext(ctx, query, opts)
	}
	return academicSearcher.SearchAcademicRaw(query, opts)
}

// retryPartialAcademic re-runs only the engines recorded as failed in a cached
// partial result, then merges the fresh hits with the cached ones.
func retryPartialAcademic(ctx context.Context, query string, opts search.AcademicSearchOptions, cached []search.SearchResult, meta academicCacheMeta) search.AcademicSearchResult {
	failed := meta.EngineErrors
	retry := make([]string, 0, len(failed))
	available := academicEngineSelection(academicSearcher.AcademicEngines(), nil)
	if len(available) == 0 {
		for name := range failed {
			retry = append(retry, name)
		}
		slices.Sort(retry)
	} else {
		for _, name := range available {
			if _, ok := failed[name]; ok {
				retry = append(retry, name)
			}
		}
	}
	if len(retry) == 0 {
		return search.AcademicSearchResult{Results: cached, EngineErrors: failed}
	}

	opts.Engines = retry
	fresh, err := callAcademicSearcher(ctx, query, opts)
	attempted := fresh.AttemptedEngines
	if attempted == nil {
		attempted = retry
	}
	remaining := make(map[string]string, len(failed))
	for name, msg := range failed {
		remaining[name] = msg
	}
	for _, name := range attempted {
		delete(remaining, name)
	}
	for name, msg := range fresh.EngineErrors {
		remaining[name] = msg
	}
	if err != nil && len(fresh.EngineErrors) == 0 && err.Error() != i18n.T("search.academic.no_results") {
		for _, name := range attempted {
			remaining[name] = err.Error()
		}
	}
	if len(remaining) == 0 {
		remaining = nil
	}
	return search.AcademicSearchResult{
		Results:          mergeAcademicResults(cached, fresh.Results),
		EngineErrors:     remaining,
		AttemptedEngines: attempted,
	}
}

func mergeAcademicResults(base, extra []search.SearchResult) []search.SearchResult {
	index := make(map[string]int, len(base)+len(extra))
	key := func(r search.SearchResult) string {
		if doi := strings.ToLower(strings.TrimSpace(r.DOI)); doi != "" {
			return "doi:" + doi
		}
		if url := strings.TrimSpace(r.Url); url != "" {
			return "url:" + url
		}
		return "title:" + strings.ToLower(strings.TrimSpace(r.Title))
	}
	merged := slices.Clone(base)
	for i, r := range merged {
		index[key(r)] = i
	}
	for _, r := range extra {
		k := key(r)
		if i, ok := index[k]; ok {
			merged[i] = r
			continue
		}
		index[k] = len(merged)
		merged = append(merged, r)
	}
	return merged
}

func storeAcademicCache(requestCache *cache.Cache, key, timeRange string, result search.AcademicSearchResult) {
	if requestCache == nil {
		return
	}
	meta := ""
	ttl := cache.TTLForFreshness(timeRange)
	if len(result.EngineErrors) != 0 {
		// 部分成功仍可复用成功引擎的结果；失败引擎在短 TTL 后重试，
		// 避免每次调用都重新打满九个学术引擎。
		ttl = min(ttl, partialAcademicTTL)
		if raw, err := json.Marshal(academicCacheMeta{EngineErrors: result.EngineErrors}); err == nil {
			meta = string(raw)
		}
	}
	if err := requestCache.StoreWithMeta(key, "", true, result.Results, "", meta, ttl); err != nil && !errors.Is(err, cache.ErrStaleGeneration) {
		log.ErrorT("log.mcp.cache_write_error_ignoring", "err", err)
	}
}

// academicCacheKey 学术搜索的缓存键：query|timeRange|engines|page|partial=v2。
// v2 起部分成功的结果也会缓存，并带 meta 记录失败引擎；旧键没有该元数据，
// 换键后重新检索，旧记录按原 TTL 淘汰。
// page 必须参与（B7）：缺页码会让 page>=2 在 TTL 内命中第一页记录，分页静默退化。
// engines 顺序保持调用方给定的顺序，不排序：引擎组合顺序本身是检索语义的一部分。
func academicCacheKey(query, timeRange string, engines []string, page int) string {
	if page <= 0 {
		page = 1
	}
	return query + "|" + timeRange + "|" + strings.Join(engines, ",") + "|page=" + strconv.Itoa(page) + "|partial=v2"
}

// academicProviderErrorsFromError extracts the adapter's "engine: message"
// summary so a fully failed aggregate call still produces provider events.
func academicProviderErrorsFromError(err error) map[string]string {
	if err == nil {
		return nil
	}
	prefix := i18n.T("search.academic.no_result_prefix")
	msg := err.Error()
	start := strings.Index(msg, prefix)
	if start < 0 {
		return nil
	}
	body := msg[start+len(prefix):]
	body = strings.TrimSuffix(body, i18n.T("search.academic.no_result_suffix"))
	out := make(map[string]string)
	for _, part := range strings.Split(body, "; ") {
		name, detail, ok := strings.Cut(part, ": ")
		if ok && name != "" {
			out[name] = detail
		}
	}
	return out
}

// academicEngineSelection infers selection for implementations without an
// authoritative attempt set; exact lookup supplies its narrower set directly.
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
func recordAcademicProviderEvents(ctx context.Context, query string, results []search.SearchResult, engineErrors map[string]string, engines []string, attemptsKnown bool, started time.Time, requestID string) {
	counts := make(map[string]int)
	failed := make(map[string]bool)
	engineTrace := make([]string, 0, len(engines))
	seenEngine := make(map[string]bool)

	mark := func(name string) {
		if name == "" || seenEngine[name] || attemptsKnown && !slices.Contains(engines, name) {
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
		telemetry.RecordEventContext(ctx, event)
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
