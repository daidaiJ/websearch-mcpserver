package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"websearch/pkg/errcode"
	"websearch/pkg/i18n"
	"websearch/pkg/search"
	searchcore "websearch/pkg/search/core"
	"websearch/pkg/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxBatchQueries = 5

type smartSearchRequest struct {
	Query          string
	Queries        []string
	Intent         string
	TimeRange      *int
	FetchTopN      *int
	Depth          string
	Sources        []string
	MaxChars       *int
	IncludeDomains []string
	ExcludeDomains []string
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
	filter, err := normalizeDomainFilters(request.IncludeDomains, request.ExcludeDomains)
	if err != nil {
		return nil, nil, err
	}
	if err := validateSourcePresets(web, academic, request.FetchTopN, request.Depth, request.MaxChars, filter); err != nil {
		return nil, nil, err
	}

	months := effectiveTimeRangeMonths(request.TimeRange)
	if len(queries) == 1 {
		out, err := searchOne(ctx, req, queries[0], request.Intent, months, fetchTopN, contentBudget, filter, web, academic, true)
		if err != nil {
			return nil, nil, err
		}
		return textResult(out.text), nil, nil
	}
	return searchBatch(ctx, req, queries, request.Intent, months, fetchTopN, contentBudget, filter, web, academic)
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

// validateSourcePresets rejects body-shaping parameters on an academic-only
// request. depth / fetch_top_n / max_chars govern web page bodies; domain
// filters likewise select web hosts. Silently ignoring them there would make
// the parameters look broken.
func validateSourcePresets(web, academic bool, fetchTopN *int, depth string, maxChars *int, filter searchcore.DomainFilter) error {
	if !academic || web {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "", "balanced":
	default:
		return errors.New(i18n.T("tool.search.preset_web_only", "Param", "depth"))
	}
	if fetchTopN != nil && *fetchTopN > 0 {
		return errors.New(i18n.T("tool.search.preset_web_only", "Param", "fetch_top_n"))
	}
	if maxChars != nil && *maxChars > 0 {
		return errors.New(i18n.T("tool.search.preset_web_only", "Param", "max_chars"))
	}
	if !filter.Empty() {
		return errors.New(i18n.T("tool.search.domain_filter_web_only"))
	}
	return nil
}

// maxDomainPatterns 单个域名名单的条目上限：过滤用于精确收窄，超长名单通常
// 意味着误用（例如把 URL 列表贴进来），拦下比静默接受更可解释。
const maxDomainPatterns = 20

func normalizeDomainFilters(include, exclude []string) (searchcore.DomainFilter, error) {
	inc, err := normalizeDomainList("include_domains", include)
	if err != nil {
		return searchcore.DomainFilter{}, err
	}
	exc, err := normalizeDomainList("exclude_domains", exclude)
	if err != nil {
		return searchcore.DomainFilter{}, err
	}
	return searchcore.DomainFilter{Include: inc, Exclude: exc}, nil
}

func normalizeDomainList(param string, raw []string) ([]string, error) {
	patterns, err := searchcore.NormalizeDomainPatterns(raw)
	if err != nil {
		var perr *searchcore.DomainPatternError
		if errors.As(err, &perr) {
			return nil, errors.New(i18n.T("tool.search.domain_invalid", "Param", param, "Domain", perr.Pattern))
		}
		return nil, err
	}
	if len(patterns) > maxDomainPatterns {
		return nil, errors.New(i18n.T("tool.search.domains_too_large", "Param", param, "Max", maxDomainPatterns, "Count", len(patterns)))
	}
	return patterns, nil
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

type smartSearchOutcome struct {
	text     string
	count    int
	engine   string
	cacheHit bool
	warning  error
}

func searchOne(ctx context.Context, req *mcp.CallToolRequest, query, intent string, months, fetchTopN, contentBudget int, filter searchcore.DomainFilter, web, academic, record bool) (out smartSearchOutcome, err error) {
	started := time.Now()
	if record {
		defer func() {
			eventErr := err
			if eventErr == nil {
				eventErr = out.warning
			}
			recordSmartSearchToolEvent(ctx, query, out.engine, eventErr == nil, started, out.cacheHit && eventErr == nil, out.count, eventErr)
		}()
	}

	var (
		webResult     *mcp.CallToolResult
		webStats      webSearchStats
		webErr        error
		academicText  string
		academicCount int
		academicErr   error
		wg            sync.WaitGroup
	)
	runAcademic := func() {
		academicText, academicCount, academicErr = runAcademicSource(ctx, query)
		if academicErr == nil && strings.TrimSpace(academicText) == "" {
			academicErr = errors.New(i18n.T("tool.search.no_valid_results"))
		}
	}
	if academic {
		if web {
			wg.Go(runAcademic)
		} else {
			runAcademic()
		}
	}
	if web {
		webResult, _, webErr = doWebSearch(ctx, req, query, intent, months, &fetchTopN,
			withMaxContentChars(contentBudget), withDomainFilter(filter), withSearchStats(&webStats), withoutToolEvent())
	}
	wg.Wait()

	if !academic && webErr != nil {
		return out, webErr
	}
	if !web && academicErr != nil {
		out.engine = "academic"
		return out, academicErr
	}
	var parts, notes []string
	if web {
		if webErr != nil {
			notes = append(notes, i18n.T("tool.search.source_web_failed", "Err", webErr))
		} else {
			parts = append(parts, callToolText(webResult))
			out.count = webStats.resultCount
			out.engine = webStats.engineName
			out.cacheHit = webStats.cacheHit
		}
	}
	if academic {
		if academicErr != nil {
			notes = append(notes, i18n.T("tool.search.source_academic_failed", "Err", academicErr))
		} else {
			parts = append(parts, i18n.T("tool.search.source_academic_header")+"\n\n"+academicText)
			out.count += academicCount
			if out.engine == "" {
				out.engine = "academic"
			}
		}
	}

	if len(notes) > 0 {
		out.warning = errcode.Message(strings.Join(notes, "; "), errors.Join(webErr, academicErr))
		out.cacheHit = false
	}
	if len(parts) == 0 {
		if out.warning != nil {
			return out, out.warning
		}
		return out, errors.New(i18n.T("tool.search.no_valid_results"))
	}
	out.text = strings.Join(append(parts, notes...), "\n\n---\n\n")
	return out, nil
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

func searchBatch(ctx context.Context, req *mcp.CallToolRequest, queries []string, intent string, months, fetchTopN, contentBudget int, filter searchcore.DomainFilter, web, academic bool) (*mcp.CallToolResult, any, error) {
	started := time.Now()
	requestID := telemetry.RequestID(ctx)
	if requestID == "" {
		requestID = telemetry.NewRequestID()
		ctx = telemetry.WithRequestID(ctx, requestID)
	}

	var (
		parts    []string
		failures []string
		causes   []error
		warnings []error
		engines  = make(map[string]struct{}, 2)
		total    int
		success  int
		allCache = true
	)
	for _, query := range queries {
		out, err := searchOne(ctx, req, query, intent, months, fetchTopN, contentBudget, filter, web, academic, false)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", query, err))
			causes = append(causes, err)
			continue
		}
		success++
		total += out.count
		allCache = allCache && out.cacheHit
		if out.engine != "" {
			engines[out.engine] = struct{}{}
		}
		parts = append(parts, out.text)
		if out.warning != nil {
			warnings = append(warnings, out.warning)
		}
	}

	failuresErr := errcode.Message(strings.Join(failures, "; "), errors.Join(causes...))
	if success == 0 {
		recordSmartSearchToolEvent(ctx, strings.Join(queries, " | "), "batch", false, started, false, 0, failuresErr)
		return nil, nil, failuresErr
	}

	provider := "batch"
	if len(engines) == 1 {
		for name := range engines {
			provider = name
		}
	}
	batchErr := errors.Join(append(warnings, failuresErr)...)
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
	storeAcademicCache(requestCache, cacheKey, "", res)
	return text, len(res.Results), nil
}
