package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"websearch/pkg/cache"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxBatchQueries = 5

type smartSearchRequest struct {
	Query     string
	Queries   []string
	Intent    string
	TimeRange *int
	FetchTopN *int
	Depth     string
	Sources   []string
	MaxChars  *int
}

// doSmartSearch resolves the per-call presets (depth / sources / batch) and
// delegates to the existing single-query pipeline.
func doSmartSearch(ctx context.Context, req *mcp.CallToolRequest, request smartSearchRequest) (*mcp.CallToolResult, any, error) {
	if telemetry.RequestID(ctx) == "" {
		ctx = telemetry.WithRequestID(ctx, telemetry.NewRequestID())
	}
	queries, err := normalizeQueries(request.Query, request.Queries)
	if err != nil {
		return nil, nil, err
	}
	fetchTopN, contentBudget, err := resolveSearchDepth(request.Depth, request.FetchTopN, request.MaxChars)
	if err != nil {
		return nil, nil, err
	}
	web, academic, err := normalizeSources(request.Sources)
	if err != nil {
		return nil, nil, err
	}

	months := effectiveTimeRangeMonths(request.TimeRange)
	if len(queries) == 1 {
		res, _, _, _, err := searchOne(ctx, req, queries[0], request.Intent, months, fetchTopN, contentBudget, web, academic, true)
		return res, nil, err
	}
	return searchBatch(ctx, req, queries, request.Intent, months, fetchTopN, contentBudget, web, academic)
}

func normalizeQueries(query string, queries []string) ([]string, error) {
	query = strings.TrimSpace(query)
	hasQueries := false
	for _, raw := range queries {
		if strings.TrimSpace(raw) != "" {
			hasQueries = true
			break
		}
	}
	if query != "" && hasQueries {
		return nil, errors.New(i18n.T("tool.search.query_queries_conflict"))
	}
	if len(queries) == 0 {
		if query == "" {
			return nil, errors.New(i18n.T("tool.search.query_required"))
		}
		return []string{query}, nil
	}

	seen := make(map[string]struct{}, len(queries))
	out := make([]string, 0, len(queries))
	for _, raw := range queries {
		q := strings.TrimSpace(raw)
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	if len(out) == 0 {
		if query != "" {
			return []string{query}, nil
		}
		return nil, errors.New(i18n.T("tool.search.query_required"))
	}
	if len(out) > maxBatchQueries {
		return nil, errors.New(i18n.T("tool.search.batch_too_large", "Max", maxBatchQueries, "Count", len(out)))
	}
	return out, nil
}

func normalizeSources(sources []string) (web, academic bool, err error) {
	if len(sources) == 0 {
		return true, false, nil
	}
	for _, raw := range sources {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "":
		case "web":
			web = true
		case "academic":
			academic = true
		default:
			return false, false, errors.New(i18n.T("tool.search.sources_invalid", "Source", raw))
		}
	}
	if !web && !academic {
		return true, false, nil
	}
	return web, academic, nil
}

// resolveSearchDepth maps the per-call preset to a fetch count and content
// budget. An explicit fetch_top_n or max_chars always wins over the preset.
func resolveSearchDepth(depth string, fetchTopN *int, maxChars *int) (int, int, error) {
	n := effectiveFetchTopN(fetchTopN)
	budget := 0
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "", "balanced":
	case "fast":
		if fetchTopN == nil {
			n = 0
		}
	case "deep":
		if fetchTopN == nil && n < 3 {
			n = 3
		}
		budget = deepSearchContentChars
	default:
		return 0, 0, errors.New(i18n.T("tool.search.depth_invalid", "Depth", depth))
	}
	if maxChars != nil {
		budget = clampContentBudget(*maxChars)
	}
	return n, budget, nil
}

func searchOne(ctx context.Context, req *mcp.CallToolRequest, query, intent string, months, fetchTopN, contentBudget int, web, academic, record bool) (*mcp.CallToolResult, int, string, bool, error) {
	started := time.Now()
	var parts []string
	var notes []string
	total := 0
	engine := ""
	cacheHit := false

	if web {
		stats := &webSearchStats{}
		res, _, err := doWebSearch(ctx, req, query, intent, months, &fetchTopN,
			withMaxContentChars(contentBudget), withSearchStats(stats), withoutToolEvent())
		if err != nil {
			if !academic {
				if record {
					recordSmartSearchToolEvent(ctx, query, "", false, started, false, 0, err)
				}
				return nil, 0, "", false, err
			}
			notes = append(notes, i18n.T("tool.search.source_web_failed", "Err", err))
		} else {
			parts = append(parts, callToolText(res))
			total += stats.resultCount
			engine = stats.engineName
			cacheHit = stats.cacheHit
		}
	}

	if academic {
		text, count, err := runAcademicSource(ctx, query)
		if err != nil {
			if !web {
				if record {
					recordSmartSearchToolEvent(ctx, query, "academic", false, started, false, 0, err)
				}
				return nil, 0, "", false, err
			}
			notes = append(notes, i18n.T("tool.search.source_academic_failed", "Err", err))
		} else if strings.TrimSpace(text) != "" {
			parts = append(parts, i18n.T("tool.search.source_academic_header")+"\n\n"+text)
			total += count
			if engine == "" {
				engine = "academic"
			}
		}
	}

	parts = append(parts, notes...)
	if len(parts) == 0 {
		err := errors.New(i18n.T("tool.search.no_valid_results"))
		if record {
			recordSmartSearchToolEvent(ctx, query, engine, false, started, false, 0, err)
		}
		return nil, 0, "", false, err
	}

	var eventErr error
	if len(notes) > 0 {
		eventErr = errors.New(strings.Join(notes, "; "))
	}
	if record {
		recordSmartSearchToolEvent(ctx, query, engine, eventErr == nil, started, cacheHit && eventErr == nil, total, eventErr)
	}
	return textResult(strings.Join(parts, "\n\n---\n\n")), total, engine, cacheHit && eventErr == nil, nil
}

func recordSmartSearchToolEvent(ctx context.Context, query, provider string, success bool, started time.Time, cacheHit bool, resultCount int, err error) {
	requestID := telemetry.RequestID(ctx)
	if requestID == "" {
		requestID = telemetry.NewRequestID()
	}
	telemetry.RecordEventContext(ctx, telemetry.Event{
		Kind: "tool", Tool: "smartsearch", Provider: provider, Query: query,
		Success: success, Duration: time.Since(started), CacheHit: cacheHit,
		ResultCount: resultCount, Error: err, RequestID: requestID,
		AttemptChain: recentAttemptChain(started, 12),
	})
}

func searchBatch(ctx context.Context, req *mcp.CallToolRequest, queries []string, intent string, months, fetchTopN, contentBudget int, web, academic bool) (*mcp.CallToolResult, any, error) {
	started := time.Now()
	requestID := telemetry.RequestID(ctx)
	if requestID == "" {
		requestID = telemetry.NewRequestID()
		ctx = telemetry.WithRequestID(ctx, requestID)
	}

	var (
		parts    []string
		failures []string
		engines  = make(map[string]struct{}, 2)
		total    int
		success  int
		allCache = true
	)
	for _, query := range queries {
		res, count, engine, cacheHit, err := searchOne(ctx, req, query, intent, months, fetchTopN, contentBudget, web, academic, false)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", query, err))
			continue
		}
		success++
		total += count
		allCache = allCache && cacheHit
		if engine != "" {
			engines[engine] = struct{}{}
		}
		parts = append(parts, callToolText(res))
	}

	if success == 0 {
		err := errors.New(strings.Join(failures, "; "))
		recordSmartSearchToolEvent(ctx, strings.Join(queries, " | "), "batch", false, started, false, 0, err)
		return nil, nil, err
	}

	provider := "batch"
	if len(engines) == 1 {
		for name := range engines {
			provider = name
		}
	}
	var batchErr error
	if len(failures) > 0 {
		batchErr = errors.New(strings.Join(failures, "; "))
	}
	recordSmartSearchToolEvent(ctx, strings.Join(queries, " | "), provider, batchErr == nil, started, allCache && batchErr == nil, total, batchErr)

	text := strings.Join(parts, "\n\n---\n\n")
	if len(failures) > 0 {
		text += "\n\n" + i18n.T("tool.search.batch_partial", "Errors", strings.Join(failures, "; "))
	}
	return textResult(text), nil, nil
}

func callToolText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func runAcademicSource(ctx context.Context, query string) (string, int, error) {
	if academicSearcher == nil {
		return "", 0, errors.New(i18n.T("tool.search.sources_academic_disabled"))
	}

	cacheKey := academicCacheKey(query, "", nil, 1)
	requestCache := requestSearchCache()
	if requestCache != nil {
		rec, hitType, err := requestCache.Lookup(cacheKey, "", true)
		if err == nil && rec != nil && rec.Academic && hitType == "query_only" {
			results, parseErr := rec.GetRawResults()
			if parseErr == nil {
				text, formatErr := formatAcademicResults(query, search.AcademicSearchResult{Results: results})
				if formatErr == nil {
					return text, len(results), nil
				}
			}
		}
	}

	started := time.Now()
	opts := search.AcademicSearchOptions{Page: 1}
	var (
		res search.AcademicSearchResult
		err error
	)
	if ctxSearcher, ok := academicSearcher.(search.ContextAcademicSearcher); ok {
		res, err = ctxSearcher.SearchAcademicRawContext(ctx, query, opts)
	} else {
		res, err = academicSearcher.SearchAcademicRaw(query, opts)
	}
	if err != nil {
		return "", 0, err
	}
	if len(res.Results) == 0 {
		return "", 0, nil
	}

	attempted := res.AttemptedEngines
	attemptsKnown := attempted != nil
	if !attemptsKnown {
		attempted = academicEngineSelection(academicSearcher.AcademicEngines(), nil)
	}
	recordAcademicProviderEvents(ctx, query, res.Results, res.EngineErrors, attempted, attemptsKnown, started, telemetry.RequestID(ctx))

	text, err := formatAcademicResults(query, res)
	if err != nil {
		return "", 0, err
	}
	if requestCache != nil {
		if storeErr := requestCache.Store(cacheKey, "", true, res.Results, "", cache.TTLForFreshness("")); storeErr != nil && !errors.Is(storeErr, cache.ErrStaleGeneration) {
			log.ErrorT("log.mcp.cache_write_error_ignoring", "err", storeErr)
		}
	}
	return text, len(res.Results), nil
}
