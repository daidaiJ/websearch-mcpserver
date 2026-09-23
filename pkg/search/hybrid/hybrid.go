package hybrid

import (
	"websearch/pkg/search/core"
	"websearch/pkg/search/enhance"

	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/telemetry"
)

const defaultEngineMaxSize = 4 // 单引擎默认最大结果数

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

func (h *HybridSearchImpl) Name() string { return "hybrid" }

func (h *HybridSearchImpl) Search(query string) (string, error) {
	results, err := h.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return h.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口，将时间范围传递给支持的子引擎。
func (h *HybridSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	var wg sync.WaitGroup
	ch := make(chan indexedResult, len(h.engines))

	for i, engine := range h.engines {
		wg.Add(1)
		go func(idx int, e core.SearchInf) {
			defer wg.Done()
			started := time.Now()
			var results []core.SearchResult
			var err error
			if timeRanger, ok := e.(core.SearchTimeRanger); ok {
				results, err = timeRanger.SearchRawWithTimeRange(query, lookbackDays)
			} else {
				results, err = e.SearchRaw(query)
			}
			telemetry.Record(telemetry.Event{Kind: "provider", Provider: e.Name(), Query: query, Success: err == nil, Duration: time.Since(started), ResultCount: len(results), Error: err})
			ch <- indexedResult{index: idx, results: results, err: err}
		}(i, engine)
	}

	wg.Wait()
	close(ch)
	return h.mergeResults(query, ch)
}

func (h *HybridSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	var wg sync.WaitGroup
	ch := make(chan indexedResult, len(h.engines))

	for i, engine := range h.engines {
		wg.Add(1)
		go func(idx int, e core.SearchInf) {
			defer wg.Done()
			started := time.Now()
			results, err := e.SearchRaw(query)
			telemetry.Record(telemetry.Event{Kind: "provider", Provider: e.Name(), Query: query, Success: err == nil, Duration: time.Since(started), ResultCount: len(results), Error: err})
			ch <- indexedResult{index: idx, results: results, err: err}
		}(i, engine)
	}

	wg.Wait()
	close(ch)
	return h.mergeResults(query, ch)
}

// mergeResults 合并多引擎搜索结果，去重、过滤、截断。
func (h *HybridSearchImpl) mergeResults(query string, ch <-chan indexedResult) ([]core.SearchResult, error) {
	seen := make(map[string]struct{})
	var merged []core.SearchResult
	var buckets []core.ScoreBucket // 评分增强所需的 per-engine 排序桶

	// 收集所有成功的结果，按引擎顺序合并
	var allResults []indexedResult
	var errSummaries []string
	for r := range ch {
		if r.err != nil {
			name := "unknown"
			if r.index >= 0 && r.index < len(h.engineNames) {
				name = h.engineNames[r.index]
			}
			// 只打引擎名与错误摘要，不输出请求细节（API Key 等）
			log.Warnf("hybrid: engine %s failed: %v", name, r.err)
			errSummaries = append(errSummaries, fmt.Sprintf("%s: %v", name, r.err))
			continue // 忽略单个引擎失败，只要有一个成功就行
		}
		allResults = append(allResults, r)
	}
	if len(allResults) == 0 {
		if len(errSummaries) > 0 {
			return nil, fmt.Errorf("所有搜索引擎均失败: %s", strings.Join(errSummaries, "; "))
		}
		return nil, fmt.Errorf("所有搜索引擎均失败")
	}

	// 按 index 排序保证结果顺序稳定
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].index < allResults[j].index
	})

	numEngines := len(h.engines)

	// 按引擎过滤 + 截断，再合并去重
	for _, ir := range allResults {
		engineName := h.engineNames[ir.index]
		ef := h.engineMap[engineName]

		// 单引擎内 URL 去重
		engineSeen := make(map[string]struct{})
		var unique []core.SearchResult
		for _, r := range ir.results {
			normalizedURL := strings.TrimSpace(r.Url)
			if _, dup := engineSeen[normalizedURL]; dup {
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
			unique = unique[:engineMax]
		}

		// 收集评分增强所需的 per-engine 排序桶
		if h.enhance {
			buckets = append(buckets, core.ScoreBucket{Name: engineName, Weight: ef.Weight, Results: unique})
		}

		// 跨引擎合并去重
		for _, r := range unique {
			normalizedURL := strings.TrimSpace(r.Url)
			if _, exists := seen[normalizedURL]; exists {
				continue
			}
			seen[normalizedURL] = struct{}{}
			merged = append(merged, r)
		}
	}

	// 评分增强流水线：RRF 融合 + 局部信号 + 多层 Boost + 阀值过滤 + MMR 重排
	if h.enhance {
		enhanced := enhance.EnhanceResultsMMR(query, buckets, h.relevanceThreshold, h.maxSize, h.mmr)
		if len(enhanced) == 0 {
			return nil, fmt.Errorf("所有搜索引擎均未返回有效结果")
		}
		return enhanced, nil
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("所有搜索引擎均未返回有效结果")
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
			merged = merged[:h.maxSize]
		} else {
			// 丢失 score 无法按 score 排序，按引擎轮询均匀保留
			merged = roundRobinByNames(merged, h.maxSize, h.engineNames)
		}
	}

	return merged, nil
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
			buf.WriteString(core.FormatMDScore(i+1, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), val.Content))
		} else {
			buf.WriteString(core.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}
