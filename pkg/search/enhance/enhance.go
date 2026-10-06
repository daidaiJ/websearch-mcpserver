package enhance

import (
	"websearch/pkg/search/core"
	"net/url"
	"sort"
	"strings"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/log"
)

// ──────────────────────────────────────────────────────────────────────────────
// Wigolo 本地评分增强流水线：RRF 融合 + 局部信号 + 多层 Boost + 阀值过滤。
// 参考 docs/native-engine-optimization.md。整个流水线为纯启发式，不依赖任何 AI 模型。
//
// 评分公式：
//   final = rrf × dq × (0.5 + 0.5×la) × rare
//   final = final + consensus_boost + authority_boost
//   final = final × recency_factor        (仅时间敏感查询)
//   保留条件: final >= threshold（Top-1 / 每引擎保底）
// ──────────────────────────────────────────────────────────────────────────────

const rrfK = 60.0 // RRF 融合常数


// RRFScore 对单个 URL 在各引擎中的排名做 Reciprocal Rank Fusion。
// ranks: engine -> rank（0 基）；K <= 0 时取默认 60。
func RRFScore(ranks map[string]int, K float64) float64 {
	if K <= 0 {
		K = rrfK
	}
	var score float64
	for _, rank := range ranks {
		score += 1.0 / (K + float64(rank))
	}
	return score
}

// engineFamilyMap 引擎家族映射：同一上游的引擎归同一 family，共识计票按 family
// 去重，避免"百度网页引擎 + 百度千帆 API"命中同一 URL 时双计共识。映射表留扩展位，
// 新增多引擎同上游场景时在此登记。
var engineFamilyMap = map[string]string{
	"baidu_api": "baidu", // 千帆 web_search 端点
	"baidu_ai":  "baidu", // 千帆 AI Search 端点
	"baidu_web": "baidu", // apipool 包装的百度网页引擎
}

// EngineFamily 返回引擎所属家族：有映射的归并到同一上游名，其余引擎名为自身 family。
func EngineFamily(name string) string {
	if f, ok := engineFamilyMap[name]; ok {
		return f
	}
	return name
}

// ConsensusBoost 多引擎共识加分（加性）。numEngines 应传入去重后的 family 数
// （见 EngineFamily），而非原始引擎数。
func ConsensusBoost(numEngines int) float64 {
	switch {
	case numEngines <= 1:
		return 0
	case numEngines == 2:
		return 0.05
	case numEngines == 3:
		return 0.10
	default:
		return 0.12
	}
}

// normalizeURLKey 归一化 URL 作为跨引擎去重键（忽略 scheme / www / 末尾斜杠）。
func normalizeURLKey(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(s, "/")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	key := host + strings.TrimRight(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	if key == "" {
		return strings.TrimRight(s, "/")
	}
	return key
}

// resultKeys 学术结果去重键组：DOI 与 URL 各生成一个键，任一键命中即视为同文。
// 仅按 DOI 单键会漏掉"一方有 DOI 一方只有相同 URL"的合并，故用双键交集匹配。
// 两键均为空时返回空（调用方应跳过该结果）。
func resultKeys(r core.SearchResult) []string {
	keys := make([]string, 0, 2)
	if doi := antirobot.NormalizeDOI(r.DOI); doi != "" {
		keys = append(keys, "doi:"+doi)
	}
	if k := normalizeURLKey(r.Url); k != "" {
		keys = append(keys, "url:"+k)
	}
	return keys
}

// urlAgg 单个去重 URL 的聚合信息。
type urlAgg struct {
	res         core.SearchResult
	rrf         float64
	engines     map[string]struct{}
	engineOrder []string
	families    map[string]struct{} // 去重后的引擎 family（同上游引擎不双计共识）
}

// EnhanceResults 对多引擎按引擎排序的结果桶执行完整评分流水线，
// 返回统一评分、去重、过滤、排序、截断后的结果（不启用 MMR 重排）。
func EnhanceResults(query string, buckets []core.ScoreBucket, threshold float64, maxSize int) []core.SearchResult {
	return EnhanceResultsMMR(query, buckets, threshold, maxSize, config.MMRConfig{})
}

// EnhanceResultsMMR 在 EnhanceResults 基础上支持 MMR 多样性重排：
// 评分排序 → ApplyScoreFloor 阀值过滤 → MMR 重排 → maxSize 截断。
// mmr.Enabled 为 false 时行为与 EnhanceResults 完全一致。
func EnhanceResultsMMR(query string, buckets []core.ScoreBucket, threshold float64, maxSize int, mmr config.MMRConfig) []core.SearchResult {
	hasError := HasErrorToken(query)
	temporal := HasTemporalIntent(query)

	order := make([]string, 0)
	m := make(map[string]*urlAgg)

	for _, b := range buckets {
		w := b.Weight
		if w <= 0 {
			w = 1.0
		}
		for rank, r := range b.Results {
			key := normalizeURLKey(r.Url)
			if key == "" {
				continue
			}
			a, ok := m[key]
			if !ok {
				a = &urlAgg{res: r, engines: make(map[string]struct{}), families: make(map[string]struct{})}
				m[key] = a
				order = append(order, key)
			} else {
				// 合并元数据：保留更完整的内容/标题/日期
				if len(r.Content) > len(a.res.Content) {
					a.res.Content = r.Content
				}
				if a.res.Title == "" && r.Title != "" {
					a.res.Title = r.Title
				}
				core.PreferDate(&a.res, r)
			}
			// RRF: 排名 0 基，与 spec 及 RRFScore 一致（顶部结果得 1/K）
			a.rrf += w * (1.0 / (rrfK + float64(rank)))
			if _, dup := a.engines[b.Name]; !dup {
				a.engines[b.Name] = struct{}{}
				a.engineOrder = append(a.engineOrder, b.Name)
				a.families[EngineFamily(b.Name)] = struct{}{}
			}
		}
	}

	scored := make([]core.SearchResult, 0, len(order))
	for _, key := range order {
		a := m[key]
		la := LexicalAlignment(query, a.res.Title, a.res.Content)
		dq := DomainQuality(a.res.Url, query, hasError)
		rare := RareTermsFactor(query, a.res.Title, a.res.Content)

		score := a.rrf * dq * (0.5 + 0.5*la) * rare
		score += ConsensusBoost(len(a.families)) // 按 family 计票：同上游引擎（如百度网页/千帆）不双计
		score += AuthorityBoost(query, a.res.Url, rare)
		score *= RecencyFactor(temporal, a.res.PublishDate)

		a.res.Score = score
		a.res.Engines = a.engineOrder
		scored = append(scored, a.res)
	}

	// 按最终分降序（稳定排序保证同分顺序确定）
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	before := len(scored)
	scored = ApplyScoreFloor(scored, threshold)

	// MMR 多样性重排（阀值过滤之后、maxSize 截断之前）
	if mmr.Enabled {
		targetN := mmr.TargetCount
		if targetN <= 0 {
			targetN = len(scored)
		}
		scored = ApplyMMR(scored, mmr.Lambda, targetN)
	}

	if maxSize > 0 && len(scored) > maxSize {
		scored = scored[:maxSize]
	}

	log.Infof("Wigolo 评分增强: intent=%s error=%v temporal=%v mmr=%v 去重后=%d 过滤后=%d 返回=%d",
		classifyIntent(query), hasError, temporal, mmr.Enabled, before, len(scored), len(scored))
	return scored
}

// ApplyScoreFloor 相关性阀值过滤，裁剪低质量上下文：
//   - score < threshold 的结果被丢弃（真正的低质量裁剪）
//   - Top-1 永不过滤（保证至少有一条最相关结果）
//   - 至少返回 2 条（若原结果 >= 2），避免过度裁剪导致上下文过空
// 输入需已按 score 降序排序。
func ApplyScoreFloor(results []core.SearchResult, threshold float64) []core.SearchResult {
	if threshold <= 0 {
		threshold = 0.05
	}
	if len(results) == 0 {
		return results
	}

	kept := make([]core.SearchResult, 0, len(results))
	for i, r := range results {
		if i == 0 { // Top-1 永不过滤
			kept = append(kept, r)
			continue
		}
		if r.Score >= threshold {
			kept = append(kept, r)
		}
	}

	// 仅剩 Top-1 时补第二条，保证最少 2 条结果
	if len(kept) < 2 && len(results) >= 2 {
		kept = append(kept, results[1])
	}
	return kept
}
