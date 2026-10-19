package hybrid

import (
	"websearch/pkg/search/core"
	"websearch/pkg/search/enhance"

	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/telemetry"
)

const defaultEngineMaxSize = 4 // 单引擎默认最大结果数

// off-topic 守卫参数：整桶回声率阈值与最小桶规模（实测校准前先取保守值，
// 只拦"整桶几乎不 echo 查询其余词"的明显诱饵页，不误伤小语种/短语查询）。
const (
	offTopicMaxEchoRatio = 0.25 // 桶内"其余词"回声率低于此值视为疑似整桶不相关
	offTopicMinReference = 0.75 // 参照桶回声率达标线（高于此值证明其余词会被正常 echo）
	offTopicMinBucket    = 3    // 桶内结果数低于此值不适用整桶守卫
	minQueryTerms        = 2    // 查询分词少于此值时无"其余词"语义，守卫不适用
)

// EngineFilter 单引擎的过滤配置。
type EngineFilter struct {
	MinScore float64 // 最低相关性分数，0 = 不过滤
	MaxSize  int     // 单引擎最大结果数，0 = 使用默认值
	Weight   float64 // 引擎权重，影响 RRF 融合分，0 = 默认 1.0
}

// HybridSearchImpl 多引擎并发搜索，支持按 score 过滤和 per-engine maxsize 截断。
type HybridSearchImpl struct {
	engines            []core.SearchInf
	engineMap          map[string]EngineFilter // 按引擎名配置的过滤规则
	maxSize            int                     // 全局最大结果数（按 score 排序后截断），0 = 不限
	engineNames        []string                // 与 engines 一一对应的引擎名
	enhance            bool                    // 是否启用 Wigolo 本地评分增强
	relevanceThreshold float64                 // 增强后的相关性阀值
	mmr                config.MMRConfig        // MMR 多样性重排配置（Enabled=false 时不重排）
	offTopicGuardMode  string                  // off-topic 守卫模式（config.OffTopicGuard*；零值 = enforce 保持直构行为，配置层缺省 shadow 经 mode/factory 注入）
	engineTimeout      time.Duration           // per-engine 软超时，0 = 只依赖请求 ctx

	diagMu   sync.Mutex             // 保护 lastDiag（并发搜索时诊断整体替换）
	lastDiag core.SearchDiagnostics // 最近一次搜索的失败诊断（实现 core.DiagnosticsProvider）
}

// engineBucket 单引擎完成去重/过滤/截断后的结果桶（off-topic 守卫与合并的中间结构）。
type engineBucket struct {
	index   int
	name    string
	results []core.SearchResult
}

// indexedResult 并发搜索时单个引擎的结果。
type indexedResult struct {
	index   int
	results []core.SearchResult
	err     error
}

func NewHybridSearch(engines ...core.SearchInf) *HybridSearchImpl {
	names := make([]string, len(engines))
	for i, e := range engines {
		names[i] = e.Name()
	}
	return &HybridSearchImpl{engines: engines, engineNames: names, engineMap: make(map[string]EngineFilter)}
}

// SetFilters 设置 per-engine 过滤配置。
func (h *HybridSearchImpl) SetFilters(engineMap map[string]EngineFilter) {
	h.engineMap = engineMap
}

// SetMaxSize 设置全局最大结果数。
func (h *HybridSearchImpl) SetMaxSize(n int) {
	h.maxSize = n
}

// SetEnhance 启用/关闭 Wigolo 本地评分增强，threshold <= 0 时使用默认 0.05。
func (h *HybridSearchImpl) SetEnhance(enabled bool, threshold float64) {
	h.enhance = enabled
	h.relevanceThreshold = threshold
}

// SetMMR 设置 MMR 多样性重排配置（仅在评分增强启用时生效）。
func (h *HybridSearchImpl) SetMMR(mmr config.MMRConfig) {
	h.mmr = mmr
}

// SetOffTopicGuard 设置 off-topic 整桶守卫模式（config.OffTopicGuard* 三态）。
func (h *HybridSearchImpl) SetOffTopicGuard(mode string) {
	h.offTopicGuardMode = mode
}

// SetEngineTimeout 设置 per-engine 软超时：超时引擎按 timeout 类型进失败清单，
// 其余引擎结果照常返回。0 = 只依赖请求级 ctx 的死线。
func (h *HybridSearchImpl) SetEngineTimeout(d time.Duration) {
	h.engineTimeout = d
}

func (h *HybridSearchImpl) Name() string { return "hybrid" }

func (h *HybridSearchImpl) Search(query string) (string, error) {
	results, err := h.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return h.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口，将时间范围传递给支持的子引擎。
// 诊断走 per-request 返回值并顺带落单槽（DiagnosticsProvider 兜底）；
// 并发请求请优先使用 SearchRawDiagnostics / SearchRawWithTimeRangeDiagnostics。
func (h *HybridSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	results, diag, err := h.SearchRawWithTimeRangeDiagnostics(query, lookbackDays)
	h.storeDiagnostics(diag)
	return results, err
}

// SearchRawWithTimeRangeDiagnostics 实现 core.DiagnosticsSearcher +
// core.SearchTimeRanger 的 per-request 诊断口径。
func (h *HybridSearchImpl) SearchRawWithTimeRangeDiagnostics(query string, lookbackDays int) ([]core.SearchResult, core.SearchDiagnostics, error) {
	return h.SearchRawWithTimeRangeContextDiagnostics(context.Background(), query, lookbackDays)
}

// SearchRawDiagnostics 实现 core.DiagnosticsSearcher：失败清单与过滤统计随本次
// 调用返回，多请求并发互不覆盖。
func (h *HybridSearchImpl) SearchRawDiagnostics(query string) ([]core.SearchResult, core.SearchDiagnostics, error) {
	return h.SearchRawContextDiagnostics(context.Background(), query)
}

// SearchRawContextDiagnostics 实现 core.ContextDiagnosticsSearcher：ctx 下传各引擎，
// 客户端断连即停止等待；诊断随返回值传递，并发请求互不覆盖。
func (h *HybridSearchImpl) SearchRawContextDiagnostics(ctx context.Context, query string) ([]core.SearchResult, core.SearchDiagnostics, error) {
	return h.run(ctx, query, 0, false)
}

// SearchRawWithTimeRangeContextDiagnostics 实现 core.ContextDiagnosticsSearcher
// 的时间范围变体。
func (h *HybridSearchImpl) SearchRawWithTimeRangeContextDiagnostics(ctx context.Context, query string, lookbackDays int) ([]core.SearchResult, core.SearchDiagnostics, error) {
	return h.run(ctx, query, lookbackDays, true)
}

// run 并发调用全部引擎并按请求 ctx / per-engine 软超时收集结果。
// withTimeRange 为 true 时优先走引擎的时间范围入口。
func (h *HybridSearchImpl) run(ctx context.Context, query string, lookbackDays int, withTimeRange bool) ([]core.SearchResult, core.SearchDiagnostics, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan indexedResult, len(h.engines))
	var wg sync.WaitGroup

	for i, engine := range h.engines {
		wg.Add(1)
		go func(idx int, e core.SearchInf) {
			defer wg.Done()
			started := time.Now()
			results, err := h.callEngine(ctx, e, query, lookbackDays, withTimeRange)
			telemetry.Record(telemetry.Event{Kind: "provider", Provider: e.Name(), Query: query, Success: err == nil, Duration: time.Since(started), ResultCount: len(results), Error: err})
			ch <- indexedResult{index: idx, results: results, err: err}
		}(i, engine)
	}

	wg.Wait()
	close(ch)
	return h.mergeResults(query, ch)
}

// errEngineTimeout 引擎未在软超时内返回（引擎自身仍在后台收尾）。
var errEngineTimeout = errors.New("引擎响应超时")

// callEngine 调用单个引擎：实现 ContextSearcher / ContextTimeRanger 的引擎直接
// 接收 ctx；旧实现者由本函数兜底——软超时或请求取消时放弃等待，按 timeout 失败
// 上报，不再阻塞整次请求。
func (h *HybridSearchImpl) callEngine(ctx context.Context, e core.SearchInf, query string, lookbackDays int, withTimeRange bool) ([]core.SearchResult, error) {
	callCtx := ctx
	if h.engineTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, h.engineTimeout)
		defer cancel()
	}

	type outcome struct {
		results []core.SearchResult
		err     error
	}
	ch := make(chan outcome, 1)
	go func() {
		results, err := h.invokeEngine(callCtx, e, query, lookbackDays, withTimeRange)
		ch <- outcome{results: results, err: err}
	}()

	select {
	case o := <-ch:
		return o.results, o.err
	case <-callCtx.Done():
		return nil, fmt.Errorf("%w: %w", errEngineTimeout, callCtx.Err())
	}
}

// invokeEngine 选择引擎支持的最强入口（ctx + 时间范围 > ctx > 时间范围 > 旧接口）。
func (h *HybridSearchImpl) invokeEngine(ctx context.Context, e core.SearchInf, query string, lookbackDays int, withTimeRange bool) ([]core.SearchResult, error) {
	if withTimeRange {
		if tr, ok := e.(core.ContextTimeRanger); ok {
			return tr.SearchRawWithTimeRangeContext(ctx, query, lookbackDays)
		}
		if tr, ok := e.(core.SearchTimeRanger); ok {
			return tr.SearchRawWithTimeRange(query, lookbackDays)
		}
	}
	if cs, ok := e.(core.ContextSearcher); ok {
		return cs.SearchRawContext(ctx, query)
	}
	return e.SearchRaw(query)
}

// SearchRaw 实现 core.SearchInf 接口。诊断走 per-request 返回值并顺带落单槽
// （DiagnosticsProvider 兜底）；并发请求请优先使用 SearchRawDiagnostics。
func (h *HybridSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	results, diag, err := h.SearchRawDiagnostics(query)
	h.storeDiagnostics(diag)
	return results, err
}

// LastDiagnostics 返回最近一次 SearchRaw / SearchRawWithTimeRange 的失败诊断，
// 实现 core.DiagnosticsProvider。诊断不写入缓存，仅随本次响应透出。
func (h *HybridSearchImpl) LastDiagnostics() core.SearchDiagnostics {
	h.diagMu.Lock()
	defer h.diagMu.Unlock()
	return h.lastDiag
}

// storeDiagnostics 整体替换最近一次诊断（并发搜索时后完成者覆盖先完成者）。
func (h *HybridSearchImpl) storeDiagnostics(d core.SearchDiagnostics) {
	h.diagMu.Lock()
	h.lastDiag = d
	h.diagMu.Unlock()
}

// mergeResults 合并多引擎搜索结果：off-topic 整桶守卫、去重、过滤、截断，
// 全程收集失败清单与过滤丢弃统计（core.SearchDiagnostics），诊断随返回值传递。
func (h *HybridSearchImpl) mergeResults(query string, ch <-chan indexedResult) ([]core.SearchResult, core.SearchDiagnostics, error) {
	var merged []core.SearchResult
	var buckets []core.ScoreBucket // 评分增强所需的 per-engine 排序桶
	diag := core.SearchDiagnostics{}

	// 收集所有成功的结果，按引擎顺序合并
	var allResults []indexedResult
	var errSummaries []string
	for r := range ch {
		if r.err != nil {
			name := "unknown"
			if r.index >= 0 && r.index < len(h.engineNames) {
				name = h.engineNames[r.index]
			}
			// 失败透出：归类（timeout/rate_limit/challenge…）+ 短原因，进失败清单
			// 与日志（只打引擎名与错误摘要，不输出请求细节 API Key 等）
			kind := core.ClassifyFailure(r.err)
			reason := core.ShortReason(r.err)
			diag.AddFailures(name, kind, reason)
			log.Warnf("hybrid: engine %s failed (%s): %s", name, kind, reason)
			errSummaries = append(errSummaries, fmt.Sprintf("%s(%s): %s", name, kind, reason))
			continue // 忽略单个引擎失败，只要有一个成功就行
		}
		allResults = append(allResults, r)
	}
	if len(allResults) == 0 {
		if len(errSummaries) > 0 {
			return nil, diag, fmt.Errorf("所有搜索引擎均失败: %s", strings.Join(errSummaries, "; "))
		}
		return nil, diag, fmt.Errorf("所有搜索引擎均失败")
	}

	// 按 index 排序保证结果顺序稳定
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].index < allResults[j].index
	})

	numEngines := len(h.engines)

	// 按引擎过滤 + 截断，产出待合并的 per-engine 桶
	var ebuckets []engineBucket
	for _, ir := range allResults {
		engineName := h.engineNames[ir.index]
		ef := h.engineMap[engineName]

		// 单引擎内 URL 去重（归一化键：同页变体只留一条）
		engineSeen := make(map[string]struct{})
		var unique []core.SearchResult
		for _, r := range ir.results {
			normalizedURL := core.NormalizeURL(r.Url)
			if _, dup := engineSeen[normalizedURL]; dup {
				diag.AddFilterDrop(core.FilterDropDedup, 1)
				continue
			}
			engineSeen[normalizedURL] = struct{}{}
			unique = append(unique, r)
		}

		// score 过滤：引擎不回传 score 时跳过 minScore 筛选
		if ef.MinScore > 0 {
			hasScore := false
			for _, r := range unique {
				if r.Score > 0 {
					hasScore = true
					break
				}
			}
			if hasScore {
				filtered := make([]core.SearchResult, 0, len(unique))
				for _, r := range unique {
					if r.Score >= ef.MinScore {
						filtered = append(filtered, r)
					} else {
						diag.AddFilterDrop(core.FilterDropMinScore, 1)
					}
				}
				unique = filtered
			}
		}

		// 单引擎 maxsize 截断
		engineMax := ef.MaxSize
		if engineMax <= 0 {
			engineMax = defaultEngineMaxSize
		}
		// 引擎不回传 score 时，取 min(engineMax, ceil(globalMax/引擎总数)) 保留最相关结果
		// 启用评分增强时跳过此均分，保留完整排序列表交由 RRF 做跨引擎融合
		if !h.enhance && h.maxSize > 0 && numEngines > 0 {
			hasScore := false
			for _, r := range unique {
				if r.Score > 0 {
					hasScore = true
					break
				}
			}
			if !hasScore {
				perEngineCap := int(math.Ceil(float64(h.maxSize) / float64(numEngines)))
				if perEngineCap < engineMax {
					engineMax = perEngineCap
				}
			}
		}
		if engineMax > 0 && len(unique) > engineMax {
			diag.AddFilterDrop(core.FilterDropMaxSize, len(unique)-engineMax)
			unique = unique[:engineMax]
		}

		ebuckets = append(ebuckets, engineBucket{index: ir.index, name: engineName, results: unique})
	}

	// off-topic 整桶守卫：先于合并执行，疑似整桶按守卫模式处置（enforce 丢弃 /
	// shadow 只记录），以 off_topic 类型进失败清单
	ebuckets = h.offTopicGuard(query, ebuckets, &diag)

	// 跨引擎合并去重；同 URL 二次出现时按日期采信规则补强（结构化来源优先，
	// 见 core.PreferDate——date_source 注记随之透传）
	seen := make(map[string]int)
	for _, b := range ebuckets {
		if h.enhance {
			buckets = append(buckets, core.ScoreBucket{Name: b.name, Weight: h.engineMap[b.name].Weight, Results: b.results})
		}
		for _, r := range b.results {
			normalizedURL := core.NormalizeURL(r.Url)
			if idx, exists := seen[normalizedURL]; exists {
				core.PreferDate(&merged[idx], r)
				continue
			}
			seen[normalizedURL] = len(merged)
			merged = append(merged, r)
		}
	}

	// 评分增强流水线：RRF 融合 + 局部信号 + 多层 Boost + 阀值过滤 + MMR 重排
	if h.enhance {
		totalIn := 0
		for _, b := range ebuckets {
			totalIn += len(b.results)
		}
		enhanced := enhance.EnhanceResultsMMR(query, buckets, h.relevanceThreshold, h.maxSize, h.mmr)
		diag.AddFilterDrop(core.FilterDropEnhance, totalIn-len(enhanced))
		if len(enhanced) == 0 {
			return nil, diag, fmt.Errorf("所有搜索引擎均未返回有效结果")
		}
		return enhanced, diag, nil
	}

	if len(merged) == 0 {
		return nil, diag, fmt.Errorf("所有搜索引擎均未返回有效结果")
	}

	// 全局 maxsize 截断（按 score 排序后）
	if h.maxSize > 0 && len(merged) > h.maxSize {
		hasScore := false
		for _, r := range merged {
			if r.Score > 0 {
				hasScore = true
				break
			}
		}
		if hasScore {
			sort.Slice(merged, func(i, j int) bool {
				return merged[i].Score > merged[j].Score
			})
			diag.AddFilterDrop(core.FilterDropGlobalMax, len(merged)-h.maxSize)
			merged = merged[:h.maxSize]
		} else {
			// 丢失 score 无法按 score 排序，按引擎轮询均匀保留
			dropped := len(merged) - h.maxSize
			merged = roundRobinByNames(merged, h.maxSize, h.engineNames)
			diag.AddFilterDrop(core.FilterDropGlobalMax, dropped)
		}
	}

	return merged, diag, nil
}

// offTopicGuard off-topic 整桶守卫：某引擎整桶几乎不 echo 查询"其余词"（排除首词）、
// 且另一引擎证明这些词会被正常 echo 时判定为疑似"HTTP 200 诱饵页"
// （返回关于查询第一个词的十个格式良好结果）。逐条裁剪之外的第二道防线，
// 判定以 off_topic 类型进失败清单（这是过滤不是墙，代理无效）。
// 三态（P1-7 回查处置）：enforce 整桶丢弃；shadow 只记录不丢弃（默认，阈值为
// 未校准初值，误杀代价 > 漏放）；off 完全关闭。守卫至少保留一个桶：
// 参照桶（回声率达标）自身不可能被判为 off-topic。
func (h *HybridSearchImpl) offTopicGuard(query string, buckets []engineBucket, diag *core.SearchDiagnostics) []engineBucket {
	mode := h.offTopicGuardMode
	if mode == "" {
		mode = config.OffTopicGuardEnforce // 直构零值维持原拦截行为；配置层缺省 shadow（config.OffTopicGuardMode）
	}
	if mode == config.OffTopicGuardOff {
		return buckets
	}

	terms := queryTerms(query)
	if len(terms) < minQueryTerms || len(buckets) < 2 {
		return buckets
	}
	other := terms[1:] // "其余词"：排除首词后的查询词项

	hasReference := false
	refEcho := 0.0
	for _, b := range buckets {
		if r := echoRatio(b.results, other); r >= offTopicMinReference {
			hasReference = true
			if r > refEcho {
				refEcho = r
			}
		}
	}
	if !hasReference {
		return buckets // 无引擎正常 echo，无法归因单个引擎，保守放行
	}

	kept := make([]engineBucket, 0, len(buckets))
	suspected := make([]engineBucket, 0, 1)
	for _, b := range buckets {
		if len(b.results) >= offTopicMinBucket && echoRatio(b.results, other) <= offTopicMaxEchoRatio {
			suspected = append(suspected, b)
			if mode == config.OffTopicGuardShadow {
				diag.AddFailures(b.name, core.FailureOffTopic,
					"shadow：整桶结果几乎不包含查询其余词，疑似不相关诱饵页；未丢弃（off_topic_guard=shadow）")
				log.Warnf("hybrid: engine %s 整桶判为 off-topic（%d 条结果，回声率过低），shadow 模式保留", b.name, len(b.results))
				kept = append(kept, b)
				continue
			}
			diag.AddFailures(b.name, core.FailureOffTopic,
				"整桶结果几乎不包含查询其余词，疑似不相关诱饵页，已整桶丢弃")
			log.Warnf("hybrid: engine %s 整桶判为 off-topic（%d 条结果，回声率过低），已丢弃", b.name, len(b.results))
			continue
		}
		kept = append(kept, b)
	}
	// 只落可疑或接近阈值的判定，避免每次正常搜索都写一条校准事件
	if len(suspected) > 0 || minEchoRatio(buckets, other) <= offTopicWatchEcho {
		recordGuardDecision(query, mode, refEcho, other, buckets, len(suspected))
	}
	return kept
}

// offTopicWatchEcho 校准回放的关注阈值：桶回声率低于此值即记录判定明细，
// 供离线回放观察阈值附近的误杀风险（阈值本身见 offTopicMaxEchoRatio）。
const offTopicWatchEcho = 0.5

func minEchoRatio(buckets []engineBucket, other []string) float64 {
	min := math.Inf(1)
	for _, b := range buckets {
		if r := echoRatio(b.results, other); r < min {
			min = r
		}
	}
	return min
}

// recordGuardDecision 落一条 guard 事件供离线校准回放（tools/guardreplay）：
// Detail 只含模式、参照回声率与各引擎桶的「回声率/条数」，不含 URL 或原文；
// 查询本身由 telemetry 层哈希与关键词化后再落库。
func recordGuardDecision(query, mode string, refEcho float64, other []string, buckets []engineBucket, suspected int) {
	var detail strings.Builder
	fmt.Fprintf(&detail, "mode=%s ref=%.3f suspected=%d", mode, refEcho, suspected)
	for _, b := range buckets {
		fmt.Fprintf(&detail, " %s=%.3f/%d", b.name, echoRatio(b.results, other), len(b.results))
	}
	telemetry.Record(telemetry.Event{Kind: "guard", Tool: "smartsearch", Query: query, Success: true, Detail: detail.String()})
}

// queryTerms 查询分词：空白/标点切分；无分隔的 CJK 长查询退化为 2 字滑窗
// （跳过首字，保留"其余词"语义）。返回 nil 表示查询过短，守卫不适用。
func queryTerms(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(fields) >= minQueryTerms {
		return fields
	}
	if len(fields) == 1 {
		runes := []rune(fields[0])
		if len(runes) >= 5 && containsCJK(runes) {
			var grams []string
			for i := 1; i+1 < len(runes); i++ {
				grams = append(grams, string(runes[i:i+2]))
			}
			if len(grams) >= minQueryTerms {
				return grams
			}
		}
	}
	return nil
}

// containsCJK 是否含中日韩统一表意文字（无分词空格，需滑窗近似）。
func containsCJK(runes []rune) bool {
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// echoRatio 桶内结果对词项集合的回声率：至少被一条结果的标题/内容命中的词项占比。
// 每条结果的匹配文本只做一次小写化（内容可能为千字节级 snippet，避免按词项重复拷贝）。
func echoRatio(results []core.SearchResult, terms []string) float64 {
	if len(terms) == 0 {
		return 1
	}
	hays := make([]string, len(results))
	for i, r := range results {
		hays[i] = strings.ToLower(r.Title + " " + r.Content)
	}
	hit := 0
	for _, t := range terms {
		lt := strings.ToLower(t)
		for _, hay := range hays {
			if strings.Contains(hay, lt) {
				hit++
				break
			}
		}
	}
	return float64(hit) / float64(len(terms))
}

// DistributeResults 按引擎轮询均匀分配结果到 limit 个（引擎名从结果的 Engine 字段提取）。
func DistributeResults(results []core.SearchResult, limit int) []core.SearchResult {
	if len(results) <= limit {
		return results
	}
	buckets := make(map[string][]core.SearchResult)
	var order []string
	seen := make(map[string]bool)
	for _, r := range results {
		e := r.Engine
		if e == "" {
			e = "_unknown"
		}
		if !seen[e] {
			seen[e] = true
			order = append(order, e)
		}
		buckets[e] = append(buckets[e], r)
	}
	return roundRobin(buckets, order, limit)
}

// roundRobinByNames 按指定引擎名顺序轮询分配结果。
func roundRobinByNames(results []core.SearchResult, limit int, engineNames []string) []core.SearchResult {
	if len(results) <= limit {
		return results
	}
	buckets := make(map[string][]core.SearchResult)
	var order []string
	seen := make(map[string]bool)
	for _, name := range engineNames {
		if !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	for _, r := range results {
		e := r.Engine
		if e == "" {
			e = "_unknown"
		}
		if !seen[e] {
			seen[e] = true
			order = append(order, e)
		}
		buckets[e] = append(buckets[e], r)
	}
	return roundRobin(buckets, order, limit)
}

// roundRobin 从分桶中轮询取结果直到达到 limit。
func roundRobin(buckets map[string][]core.SearchResult, order []string, limit int) []core.SearchResult {
	var out []core.SearchResult
	indices := make(map[string]int)
	for len(out) < limit {
		added := false
		for _, name := range order {
			if len(out) >= limit {
				break
			}
			idx := indices[name]
			if idx < len(buckets[name]) {
				out = append(out, buckets[name][idx])
				indices[name] = idx + 1
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out
}

func (h *HybridSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有结果可合并")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if core.ShowMeta {
			buf.WriteString(core.FormatMDScore(i+1, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), core.FormatDateSource(val.PublishDate, val.DateSource), val.Content))
		} else {
			buf.WriteString(core.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}
