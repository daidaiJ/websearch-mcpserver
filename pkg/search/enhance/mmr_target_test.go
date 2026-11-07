package enhance

import (
	"fmt"
	"sort"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search/core"
)

// ── MMR target_count 默认跟随 max_size ──────────────────────────────────────

// replayMMRWithOldTargetN 按旧实现口径跑 MMR 段：targetN = len(scored)。
func replayMMRWithOldTargetN(scored []core.SearchResult, lambda float64, maxSize int) []core.SearchResult {
	targetN := len(scored)
	out := ApplyMMR(scored, lambda, targetN)
	if maxSize > 0 && len(out) > maxSize {
		out = out[:maxSize]
	}
	return out
}

// genBuckets 生成 n 个结果的桶：编号 i 越大 score 越低；主题在
// sameCount 与其余（每 3 条一个新主题）之间切换，制造 MMR 可观测的重排空间。
func genBuckets(n, sameCount int) []core.ScoreBucket {
	var results []core.SearchResult
	for i := 0; i < n; i++ {
		topic := "alpha beta gamma"
		if i >= sameCount {
			topic = fmt.Sprintf("delta epsilon zeta eta %d", i)
		}
		results = append(results, core.SearchResult{
			Title:   fmt.Sprintf("Result %d %s", i, topic),
			Url:     fmt.Sprintf("https://example.com/r%d", i),
			Content: fmt.Sprintf("result %d body %s", i, topic),
			Engine:  "bing",
		})
	}
	return []core.ScoreBucket{{Name: "bing", Weight: 1.0, Results: results}}
}

// replayScoring 复刻 EnhanceResultsMMR 的评分段（RRF 融合 + 局部信号 +
// 共识/权威 Boost + 新鲜度 + 降序排序），供等价性用例在不依赖新实现入口的
// 前提下重建旧实现所需的 scored 输入。生产代码这段在本批未改，复刻与
// 实现漂移时用例会直接暴露不一致。
func replayScoring(query string, buckets []core.ScoreBucket) []core.SearchResult {
	hasError := HasErrorToken(query)
	temporal := HasTemporalIntent(query)

	type agg struct {
		res      core.SearchResult
		rrf      float64
		engines  map[string]struct{}
		families map[string]struct{}
	}
	m := make(map[string]*agg)
	var order []string
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
				a = &agg{res: r, engines: make(map[string]struct{}), families: make(map[string]struct{})}
				m[key] = a
				order = append(order, key)
			} else if len(r.Content) > len(a.res.Content) {
				a.res.Content = r.Content
			}
			a.rrf += w * (1.0 / (rrfK + float64(rank)))
			a.engines[b.Name] = struct{}{}
			a.families[EngineFamily(b.Name)] = struct{}{}
		}
	}
	var scored []core.SearchResult
	for _, key := range order {
		a := m[key]
		la := LexicalAlignment(query, a.res.Title, a.res.Content)
		dq := DomainQuality(a.res.Url, query, hasError)
		rare := RareTermsFactor(query, a.res.Title, a.res.Content)
		score := a.rrf * dq * (0.5 + 0.5*la) * rare
		score += ConsensusBoost(len(a.families))
		score += AuthorityBoost(query, a.res.Url, rare)
		score *= RecencyFactor(temporal, a.res.PublishDate)
		a.res.Score = score
		scored = append(scored, a.res)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	return scored
}

// TestMMRTargetCountFollowsMaxSizeEquivalent target_count 缺省时新实现
// （targetN = maxSize）与旧实现（targetN = len(scored)）在启用 maxSize 截断时
// 输出逐条相同——依据 MMR 贪心选择的前缀性（选前 k 项 = 对全量选择后的前 k 项）。
// 阀值取 0.001 而非 0：0 会被 ApplyScoreFloor 归一成 0.05 把本语料全部裁掉
// （只剩 Top-1 保底），新旧两条路径一致地都只剩 2 条，等价性对比就失去意义了。
// 覆盖 maxSize ∈ {1, 3, 10, 25} 与 target_count > 0 两条路径。
// 把生产代码回退成旧实现（targetN = len(scored)）时本用例仍通过，
// 等价性由测试固定而不是被测试替代。
func TestMMRTargetCountFollowsMaxSizeEquivalent(t *testing.T) {
	const threshold = 0.001
	for _, maxSize := range []int{1, 3, 10, 25} {
		buckets := genBuckets(40, 12)
		scored := replayScoring("alpha beta gamma", buckets)
		// 旧实现：按旧 targetN 语义（len(scored)）对全量跑 ApplyMMR，再套同样的
		// maxSize 截断。这里不能走 EnhanceResultsMMR（它已是新实现），故复刻旧
		// 路径的评分产出：EnhanceResultsMMR 的评分段（RRF 融合 + 各信号 + 排序）
		// 在本批改动中未变，用同样的逻辑在测试内重放得到 scored。
		scored = ApplyScoreFloor(scored, threshold)
		oldMMR := replayMMRWithOldTargetN(scored, 0.7, maxSize)
		newOut := EnhanceResultsMMR("alpha beta gamma", buckets, threshold, maxSize, config.MMRConfig{Enabled: true, Lambda: 0.7})
		if len(newOut) != maxSize {
			t.Errorf("maxSize=%d: 新实现应截断到 %d 条, got %d", maxSize, maxSize, len(newOut))
		}
		if len(oldMMR) != len(newOut) {
			t.Fatalf("maxSize=%d: 条数不一致, old=%d new=%d", maxSize, len(oldMMR), len(newOut))
		}
		for i := range oldMMR {
			if oldMMR[i].Url != newOut[i].Url || oldMMR[i].Score != newOut[i].Score {
				t.Errorf("maxSize=%d: 第 %d 条不一致, old=(%s,%.6f) new=(%s,%.6f)",
					maxSize, i, oldMMR[i].Url, oldMMR[i].Score, newOut[i].Url, newOut[i].Score)
			}
		}
	}
	// target_count > 0 时仍以它为上限（两实现相同分支，行为不变）：
	// target_count=4、maxSize=10 → MMR 输出 4 条，截断不生效。
	got := EnhanceResultsMMR("alpha beta gamma", genBuckets(40, 12), 0.001, 10, config.MMRConfig{Enabled: true, Lambda: 0.7, TargetCount: 4})
	if len(got) != 4 {
		t.Errorf("target_count=4 应返回 4 条, got %d", len(got))
	}
}

// TestMMRTargetCountFollowsMaxSizeNoExtraTruncate maxSize <= 0（全局不限）时
// 维持旧语义：targetN = len(scored)，MMR 不截断，全部结果返回。
// 阀值取 0.001（0 会被 ApplyScoreFloor 归一成 0.05 裁掉全部结果）。
func TestMMRTargetCountFollowsMaxSizeNoExtraTruncate(t *testing.T) {
	buckets := genBuckets(40, 12)
	got := EnhanceResultsMMR("alpha beta gamma", buckets, 0.001, 0, config.MMRConfig{Enabled: true, Lambda: 0.7})
	if len(got) != 40 {
		t.Errorf("maxSize=0 应保留全部 40 条, got %d", len(got))
	}
}

// TestMMRTailBeyondMaxSizeNotProcessed MMR 不再处理必定被丢弃的尾部：
// target_count 缺省时目标条数跟随 maxSize（旧实现为全部输入条数，
// 对 40 条输入做二次方贪心选择后又被截断丢弃）。
func TestMMRTailBeyondMaxSizeNotProcessed(t *testing.T) {
	const total = 40
	if got := mmrTargetN(config.MMRConfig{}, 5, total); got != 5 {
		t.Errorf("target_count 缺省 + maxSize=5 时目标条数应为 5, got %d", got)
	}
	if got := mmrTargetN(config.MMRConfig{}, 0, total); got != total {
		t.Errorf("maxSize 不限时应重排全部 %d 条, got %d", total, got)
	}
	if got := mmrTargetN(config.MMRConfig{TargetCount: 30}, 5, total); got != 30 {
		t.Errorf("target_count 显式配置应以其为准, got %d", got)
	}
	if got := mmrTargetN(config.MMRConfig{TargetCount: 30}, 5, 10); got != 10 {
		t.Errorf("target_count 超过输入条数时应取输入条数, got %d", got)
	}
}
