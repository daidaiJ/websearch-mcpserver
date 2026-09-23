package mcpserver

import (
	"fmt"
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"websearch/pkg/cache"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/log"
	"websearch/pkg/search"
	searchcore "websearch/pkg/search/core"
	"websearch/pkg/telemetry"
)

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。

// smartsearch 工具：handler、搜索编排、缓存、fetch_top_n 与单引擎过滤。
// ── WebSearch 处理函数（两个版本适配不同 Params） ─────────────────────────────

// WebSearchWithIntent LLM 启用时的 tool handler。
func WebSearchWithIntent(ctx context.Context, req *mcp.CallToolRequest, params *SearchParamsWithIntent) (*mcp.CallToolResult, any, error) {
	return doWebSearch(ctx, req, params.Query, params.Intent, params.TimeRange, params.FetchTopN)
}

// WebSearchNoIntent LLM 未启用时的 tool handler。
func WebSearchNoIntent(ctx context.Context, req *mcp.CallToolRequest, params *SearchParamsNoIntent) (*mcp.CallToolResult, any, error) {
	return doWebSearch(ctx, req, params.Query, "", params.TimeRange, params.FetchTopN)
}

// doWebSearch 通用网页搜索逻辑。
// timeRangeMonths 控制搜索时间范围（月），默认 3，0 表示不限。
// 摘要阶段优先流式推送（MCP progress notification），客户端可实时看到生成过程。
func doWebSearch(ctx context.Context, req *mcp.CallToolRequest, query, intent string, timeRangeMonths int, fetchTopN *int) (result *mcp.CallToolResult, extra any, err error) {
	started := time.Now()
	engineName := ""
	cacheHit := false
	resultCount := 0
	requestID := telemetry.RequestID(ctx)
	if requestID == "" {
		requestID = telemetry.NewRequestID()
		ctx = telemetry.WithRequestID(ctx, requestID)
	}
	defer func() {
		telemetry.RecordEventContext(ctx, telemetry.Event{Kind: "tool", Tool: "smartsearch", Provider: engineName, Query: query, Success: err == nil, Duration: time.Since(started), CacheHit: cacheHit, ResultCount: resultCount, Error: err, RequestID: requestID, AttemptChain: recentAttemptChain(started, 12)})
		if !cacheHit && engineName != "" && engineName != "hybrid" && engineName != "apipool" {
			telemetry.Record(telemetry.Event{Kind: "provider", Provider: engineName, Query: query, Success: err == nil, Duration: time.Since(started), ResultCount: resultCount, Error: err, RequestID: requestID})
		}
	}()
	if searchapi == nil {
		return nil, nil, fmt.Errorf("api 初始化未完成")
	}
	engineName = searchapi.Name()

	n := effectiveFetchTopN(fetchTopN)

	// 默认3个月
	if timeRangeMonths == 0 {
		timeRangeMonths = 3
	}
	lookbackDays := timeRangeMonths * 30
	cacheQuery := webSearchCacheQuery(query, n)

	// ---- 缓存查询 ----
	if cacheInst != nil {
		rec, hitType, err := cacheInst.Lookup(cacheQuery, intent, false)
		if err != nil {
			log.Errf("缓存查询异常，跳过缓存: %v", err)
		} else if rec != nil && !rec.Academic {
			if result, ok := finishCachedWebSearch(ctx, rec, hitType, query, intent, n); ok {
				cacheHit = true
				if parsed, parseErr := rec.GetRawResults(); parseErr == nil {
					resultCount = len(parsed)
				}
				return result, nil, nil
			}
		}
		if n > 0 {
			// n 专用 key 未命中时，用未抽取的缓存补抽，避免把无正文结果当成完整命中
			rec, hitType, err := cacheInst.Lookup(query, intent, false)
			if err == nil && rec != nil && !rec.Academic && hitType == "query_only" {
				results, parseErr := rec.GetRawResults()
				if parseErr == nil {
					cacheHit = true
					resultCount = len(results)
					results = enrichFetchedTopN(ctx, results, n)
					return finishWebSearch(ctx, req, query, intent, cacheQuery, results)
				}
			}
		}
	}

	// ---- 搜索 ----
	var results []search.SearchResult

	// 优先使用支持时间范围的接口
	if timeRanger, ok := searchapi.(search.SearchTimeRanger); ok {
		results, err = timeRanger.SearchRawWithTimeRange(query, lookbackDays)
	} else {
		results, err = searchapi.SearchRaw(query)
	}
	if err != nil {
		if fallbackSearch != nil && searchapi != fallbackSearch {
			log.Errf("主搜索引擎失败(%v)，回退到 Bing 引擎", err)
			results, err = fallbackSearch.SearchRaw(query)
			engineName = "bing"
		}
		if err != nil {
			return nil, nil, err
		}
	}

	// 单引擎模式下应用 smartsearch 过滤（HybridSearchImpl 已在 SearchRaw 内处理）
	if _, isHybrid := searchapi.(*search.HybridSearchImpl); !isHybrid {
		results = postSearchFilter(results, engineName)
	}

	results = enrichFetchedTopN(ctx, results, n)
	resultCount = len(results)
	return finishWebSearch(ctx, req, query, intent, cacheQuery, results)
}

const maxFetchTopN = 5

func clampFetchTopN(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxFetchTopN {
		return maxFetchTopN
	}
	return n
}

// effectiveFetchTopN 解析生效的抓取条数（优先级：agent 显式传参 > 服务端配置）：
//   - nil（agent 未传）：用 smartsearch.fetch_top_n —— 用户启用后默认一次搜索即含正文；
//   - 0（agent 明确要轻量）：只保留标题摘要，正文之后按需补抓；
//   - 1-5：并发抓取前 N 条正文（超过 5 钳制到 5）。
func effectiveFetchTopN(param *int) int {
	if param != nil {
		return clampFetchTopN(*param)
	}
	return clampFetchTopN(smartSearchConf.FetchTopN)
}

func webSearchCacheQuery(query string, n int) string {
	if n <= 0 {
		return query
	}
	return query + "|fetch_top_n=" + strconv.Itoa(n)
}

func finishCachedWebSearch(_ context.Context, rec *cache.CacheRecord, hitType, query, intent string, fetchTopN int) (*mcp.CallToolResult, bool) {
	switch hitType {
	case "exact_intent":
		if rec.Summary != "" {
			log.Infof("缓存命中(exact_intent+summary): query=%s", query)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: rec.Summary}}}, true
		}
		fallthrough
	case "query_only":
		results, parseErr := rec.GetRawResults()
		if parseErr != nil {
			return nil, false
		}
		log.Infof("缓存命中(query_only): query=%s", query)
		ret, mergeErr := formatRawResults(query, results)
		if mergeErr != nil {
			return nil, false
		}
		if intent != "" && summarizerInst != nil && rec.Summary == "" {
			cacheQuery := webSearchCacheQuery(query, fetchTopN)
			go asyncSummarize(query, cacheQuery, intent, results)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, true
	}
	return nil, false
}

func asyncSummarize(query, cacheQuery, intent string, results []search.SearchResult) {
	defer func() {
		if r := recover(); r != nil {
			log.Errf("异步摘要 panic: %v", r)
		}
	}()
	output, sumErr := summarizerInst.Summarize(query, intent, results)
	if sumErr == nil && cacheInst != nil {
		_ = cacheInst.UpdateSummary(cacheQuery, intent, output)
		log.Infof("后台异步摘要完成: query=%s, intent=%s", query, intent)
	}
}

func finishWebSearch(ctx context.Context, req *mcp.CallToolRequest, query, intent, cacheQuery string, results []search.SearchResult) (*mcp.CallToolResult, any, error) {
	if intent != "" && summarizerInst != nil {
		var output string
		var sumErr error
		if req != nil && req.Session != nil {
			output, sumErr = streamSummarize(ctx, req, query, intent, results)
			if sumErr != nil {
				log.Errf("LLM 流式摘要失败，回退到非流式摘要: %v", sumErr)
			}
		}
		if sumErr != nil {
			output, sumErr = summarizerInst.Summarize(query, intent, results)
		}
		if sumErr == nil {
			if cacheInst != nil {
				_ = cacheInst.Store(cacheQuery, intent, false, results, output)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
		}
		log.Errf("LLM 摘要失败，回退到原始结果: %v", sumErr)
	}

	ret, err := formatRawResults(query, results)
	if err != nil {
		return nil, nil, err
	}
	if cacheInst != nil {
		_ = cacheInst.Store(cacheQuery, intent, false, results, "")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

type pageFetcher func(ctx context.Context, rawURL string) (string, error)

// minContentLenForFetch 结果已有足量正文（如 Tavily raw_content / Exa text）时
// 跳过二次抓取：fetch_top_n 只补 HTML 引擎摘要条目的正文。
const minContentLenForFetch = 1000

func enrichTopN(ctx context.Context, results []search.SearchResult, n int, fetch pageFetcher) []search.SearchResult {
	if n <= 0 || fetch == nil || len(results) == 0 {
		return results
	}
	if n > len(results) {
		n = len(results)
	}
	out := make([]search.SearchResult, len(results))
	copy(out, results)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		u := strings.TrimSpace(out[i].Url)
		if u == "" {
			continue
		}
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			// 已有足量正文（API raw_content/text）的条目跳过二次抓取
			if len(strings.TrimSpace(out[i].Content)) >= minContentLenForFetch {
				return
			}
			body, err := fetch(ctx, u)
			if err != nil {
				log.Warnf("fetch_top_n 抓取失败: %s: %v", u, err)
				out[i].Content = annotateFetchFailure(out[i].Content, err)
				return
			}
			if strings.TrimSpace(body) == "" {
				log.Warnf("fetch_top_n 抓取结果为空: %s", u)
				out[i].Content = annotateFetchFailure(out[i].Content, errors.New("页面内容为空(可能被反爬)"))
				return
			}
			out[i].Content = body
		}(i, u)
	}
	wg.Wait()
	return out
}

// annotateFetchFailure 抓取失败时把原因显式标注在结果上，防止 agent 把
// snippet 当成正文；JS 挑战 / WAF / 验证码类防护单独点明。
func annotateFetchFailure(snippet string, err error) string {
	var sb strings.Builder
	if s := strings.TrimSpace(snippet); s != "" {
		sb.WriteString(s)
		sb.WriteString("\n\n")
	}
	if isAntiBotFailure(err) {
		sb.WriteString("> ⚠️ 正文抓取被网站反爬防护拦截（JS 挑战/WAF/验证码）：无法获取该页面全文，仅有以上摘要与 URL。请改用其它来源，或稍后用 cleanfetch 重试该页面。")
	} else {
		fmt.Fprintf(&sb, "> ⚠️ 正文抓取失败（%v）：仅有以上摘要与 URL。如需正文可用 cleanfetch 重试该页面。", err)
	}
	return sb.String()
}

// isAntiBotFailure 判断抓取错误是否为反爬防护类。
// webfetch.Fetch 已把底层错误分类为中文描述（webfetch.go classifyError），这里按关键词识别。
func isAntiBotFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "反爬") || strings.Contains(msg, "WAF") ||
		strings.Contains(msg, "挑战") || strings.Contains(msg, "验证码")
}

// fetchPageContent fetch_top_n 的单条抓取路径：SSRF 预检 + HEAD 体积预检 +
// webfetch 抓取 + 字节上限截断，与 cleanfetch 的 fetchCleanPage 同一套防线（F4）。
// webfetch 未就绪时惰性初始化，仍失败则报错（fail closed，F1）。
func fetchPageContent(ctx context.Context, rawURL string) (string, error) {
	if err := validateURLSecurity(rawURL); err != nil {
		return "", err
	}
	if err := headCheck(ctx, rawURL); err != nil {
		return "", err
	}
	if !ensureWebFetch() {
		return "", fmt.Errorf("webfetch 未初始化，fetch_top_n 需要 cleanfetch/pdf_parser 至少启用其一")
	}
	res, err := webfetchInst.Fetch(ctx, rawURL)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", fmt.Errorf("empty content")
	}
	// 大正文被 webfetch 落盘（超过 max_inline_lines）：Markdown 为空但抓取成功，
	// 返回文件路径与读取提示，不能误判为抓取失败。
	if res.Mode == "saved_to_file" && res.FilePath != "" {
		return formatSavedToFileResult(res), nil
	}
	if strings.TrimSpace(res.Markdown) == "" {
		return "", fmt.Errorf("empty content")
	}
	return truncateFetchContent(res.Markdown), nil
}

// formatSavedToFileResult 将 saved_to_file 模式结果格式化进搜索结果条目，
// 格式与 cleanfetch 的大文本输出一致：标题 + 统计 + 文件路径 + 读取提示。
func formatSavedToFileResult(res *webfetch.Result) string {
	var sb strings.Builder
	if res.Title != "" {
		sb.WriteString(res.Title)
		sb.WriteString("\n\n")
	}
	fmt.Fprintf(&sb, "正文较长（共 %d 行，%d 字符），已保存到文件\n\n", res.TotalLines, res.TotalChars)
	fmt.Fprintf(&sb, "**文件路径**: `%s`", res.FilePath)
	if res.AgentHint != "" {
		fmt.Fprintf(&sb, "\n\n**读取提示**: %s", res.AgentHint)
	}
	sb.WriteString("\n")
	return sb.String()
}

// truncateFetchContent 将超长正文截断到 cleanfetch.max_fetch_size_mb（默认 10MB），
// 防止搜索引擎命中的大二进制页面把整次搜索结果撑爆。
func truncateFetchContent(body string) string {
	maxSizeMB := cleanFetchMaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = 10
	}
	limit := maxSizeMB * 1024 * 1024
	if len(body) <= limit {
		return body
	}
	log.Warnf("fetch_top_n 正文超过 %dMB，已截断", maxSizeMB)
	return body[:limit] + "\n\n（正文超过大小上限，已截断）"
}

func enrichFetchedTopN(ctx context.Context, results []search.SearchResult, n int) []search.SearchResult {
	if n <= 0 {
		return results
	}
	fetchCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		fetchCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
	}
	defer cancel()
	return enrichTopN(fetchCtx, results, n, fetchPageContent)
}

// HybridSearchImpl 已在 SearchRaw 内处理，此函数仅用于单引擎模式。
func postSearchFilter(results []search.SearchResult, engineName string) []search.SearchResult {
	if len(results) == 0 {
		return results
	}
	ec := smartSearchConf.Engines[engineName]

	// score 过滤
	results = searchcore.FilterByScore(results, ec.MinScore)

	// 单引擎 maxsize 截断：仅应用显式配置的 per-engine max_size。
	// 未配置（MaxSize<=0）时不再回落默认 4 —— defaultEngineMaxSize 只属于 hybrid
	// 编排层（pkg/search/hybrid.go）的 per-engine 缺省过滤；tool 层若也回落 4，
	// 会覆盖 apipool / baidu_ai 等未在 smartsearch.engines 配置的引擎在
	// factory.go 里 SetMaxSize 的全局截断。未配置时交给全局 max_size 或引擎自身截断。
	engineMax := ec.MaxSize
	if engineMax <= 0 {
		engineMax = 0
	}
	// 引擎不回传 score 时，取 min(engineMax, ceil(globalMax/1))
	if smartSearchConf.MaxSize > 0 {
		hasScore := false
		for _, r := range results {
			if r.Score > 0 {
				hasScore = true
				break
			}
		}
		if !hasScore {
			perEngineCap := smartSearchConf.MaxSize // 单引擎时 ceil(maxSize/1) = maxSize
			if perEngineCap < engineMax {
				engineMax = perEngineCap
			}
		}
	}
	if engineMax > 0 && len(results) > engineMax {
		results = results[:engineMax]
	}

	// 全局 maxsize 截断
	if smartSearchConf.MaxSize > 0 && len(results) > smartSearchConf.MaxSize {
		searchcore.SortByScore(results)
		results = results[:smartSearchConf.MaxSize]
	}

	return results
}
