package enhance

import (
	"websearch/pkg/search/core"
	"math"
	"sort"
	"strings"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
)

// ──────────────────────────────────────────────────────────────────────────────
// 学术搜索评分增强流水线：RRF 融合 + 学术特有信号 + 阀值过滤。
//
// 评分公式：
//   final = rrf × cite_factor
//   final = final + journal_boost + pdf_boost
//   final = final × recency_factor      (仅时间敏感查询)
//   保留条件: final >= threshold（Top-1 / 每引擎保底）
//
// 学术搜索不适用通用搜索的 DomainQuality / LexicalAlignment / RareTermsFactor
// （域名均为 .org/.edu，snippet 为英文摘要），改用引用数/期刊权威/PDF 可用性/
// 新鲜度等学术特有信号。
// ──────────────────────────────────────────────────────────────────────────────

// pdfBoost PDF 可用性加性加分（有全文链接的论文实用性更高）。
const pdfBoost = 0.03

// highImpactVenues 已知高影响力期刊/会议 → 加性加分。
var highImpactVenues = map[string]float64{
	"nature":                0.08,
	"science":               0.08,
	"cell":                  0.07,
	"the lancet":            0.07,
	"nejm":                  0.07,
	"pnas":                  0.05,
	"nature communications": 0.05,
	"iclr":                  0.05,
	"neurips":               0.05,
	"icml":                  0.05,
	"cvpr":                  0.05,
	"acl":                   0.04,
	"emnlp":                 0.04,
	"sigir":                 0.04,
	"www":                   0.04,
	"kdd":                   0.04,
	"osdi":                  0.05,
	"sosp":                  0.05,
	"pldi":                  0.04,
	"popl":                  0.04,
}

// CiteFactor 引用数增强因子：对数压缩避免高引论文完全碾压，clamp 到 [1.0, 1.7]。
func CiteFactor(citedBy int) float64 {
	if citedBy <= 0 {
		return 1.0
	}
	f := 1 + math.Log2(1+float64(citedBy))*0.05
	if f < 1.0 {
		return 1.0
	}
	if f > 1.7 {
		return 1.7
	}
	return f
}

// JournalBoost 期刊权威 Boost：高影响力期刊/会议加性加分（大小写不敏感匹配）。
func JournalBoost(journal string) float64 {
	if journal == "" {
		return 0
	}
	lower := strings.ToLower(journal)
	best := 0.0
	for venue, boost := range highImpactVenues {
		if strings.Contains(lower, venue) && boost > best {
			best = boost
		}
	}
	return best
}

// AcademicRecencyFactor 学术新鲜度因子：仅时间敏感查询生效，窗口比通用搜索更宽松。
//   < 1 年 ×1.15 | < 3 年 ×1.08 | < 5 年 ×1.03 | ≥ 5 年或无日期 ×1.00
func AcademicRecencyFactor(temporal bool, publishDate string) float64 {
	if !temporal {
		return 1.0
	}
	t, ok := parsePublishDate(publishDate)
	if !ok {
		return 1.0
	}
	days := time.Since(t).Hours() / 24
	switch {
	case days < 0:
		return 1.0 // 未来日期，忽略
	case days < 365:
		return 1.15
	case days < 365*3:
		return 1.08
	case days < 365*5:
		return 1.03
	default:
		return 1.0
	}
}

// EnhanceAcademicResults 对六大学术引擎的 per-engine 结果桶执行学术评分流水线：
// RRF 融合（复用 rrfK）→ 引用数/期刊/PDF/新鲜度信号 → 阀值过滤（含每引擎保底）。
// 引擎回传 score（OpenAlex / Semantic Scholar）> 0 时按 score 降序作为该引擎排名，
// 否则按返回顺序排名。
func EnhanceAcademicResults(query string, buckets []core.ScoreBucket, threshold float64, maxSize int) []core.SearchResult {
	temporal := HasTemporalIntent(query)

	order := make([]*urlAgg, 0)
	// 双键索引：同一聚合条目同时登记 DOI 键与 URL 键，任一键命中即合并
	byDOI := make(map[string]*urlAgg)
	byURL := make(map[string]*urlAgg)

	for _, b := range buckets {
		w := b.Weight
		if w <= 0 {
			w = 1.0
		}
		for rank, r := range b.Results {
			doiKey := antirobot.NormalizeDOI(r.DOI)
			urlKey := normalizeURLKey(r.Url)
			if doiKey == "" && urlKey == "" {
				continue
			}
			a := byDOI[doiKey]
			if a == nil {
				a = byURL[urlKey]
			}
			if a == nil {
				a = &urlAgg{res: r, engines: make(map[string]struct{})}
				order = append(order, a)
			}
			if doiKey != "" {
				byDOI[doiKey] = a
			}
			if urlKey != "" {
				byURL[urlKey] = a
			}
			// 合并元数据：保留更完整的摘要/标题/日期，引用数取较大值
			if len(r.Content) > len(a.res.Content) {
				a.res.Content = r.Content
			}
			if a.res.Title == "" && r.Title != "" {
				a.res.Title = r.Title
			}
			core.PreferDate(&a.res, r)
			if r.CitedBy > a.res.CitedBy {
				a.res.CitedBy = r.CitedBy
			}
			if a.res.Journal == "" && r.Journal != "" {
				a.res.Journal = r.Journal
			}
			if a.res.PDFURL == "" && r.PDFURL != "" {
				a.res.PDFURL = r.PDFURL
			}
			if a.res.DOI == "" && r.DOI != "" {
				a.res.DOI = r.DOI
			}
			a.rrf += w * (1.0 / (rrfK + float64(rank)))
			if _, dup := a.engines[b.Name]; !dup {
				a.engines[b.Name] = struct{}{}
				a.engineOrder = append(a.engineOrder, b.Name)
			}
		}
	}

	scored := make([]core.SearchResult, 0, len(order))
	for _, a := range order {
		score := a.rrf * CiteFactor(a.res.CitedBy)
		score += JournalBoost(a.res.Journal)
		if a.res.PDFURL != "" {
			score += pdfBoost
		}
		score *= AcademicRecencyFactor(temporal, a.res.PublishDate)

		a.res.Score = score
		a.res.Engines = a.engineOrder
		scored = append(scored, a.res)
	}

	// 按最终分降序（稳定排序保证同分顺序确定）
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	before := len(scored)
	scored = applyAcademicScoreFloor(scored, buckets, threshold)

	if maxSize > 0 && len(scored) > maxSize {
		scored = scored[:maxSize]
	}

	log.Infof("学术评分增强: temporal=%v 去重后=%d 过滤后=%d 返回=%d",
		temporal, before, len(scored), len(scored))
	return scored
}

// applyAcademicScoreFloor 学术版阀值过滤：
//   - score < threshold 的结果被丢弃
//   - Top-1 永不过滤
//   - 每引擎保底：引擎内排名第 1 的结果即使低于阀值也恢复
// 输入需已按 score 降序排序。
func applyAcademicScoreFloor(scored []core.SearchResult, buckets []core.ScoreBucket, threshold float64) []core.SearchResult {
	if threshold <= 0 {
		threshold = 0.02
	}
	if len(scored) == 0 {
		return scored
	}

	kept := make([]core.SearchResult, 0, len(scored))
	keptKeys := make(map[string]struct{}, len(scored)*2)
	addKeys := func(r core.SearchResult) {
		for _, k := range resultKeys(r) {
			keptKeys[k] = struct{}{}
		}
	}
	for i, r := range scored {
		if i == 0 { // Top-1 永不过滤
			kept = append(kept, r)
			addKeys(r)
			continue
		}
		if r.Score >= threshold {
			kept = append(kept, r)
			addKeys(r)
		}
	}

	// 每引擎保底：引擎内第一名若被过滤则恢复（带原 score，保持排序）。
	// 命中判定与合并逻辑一致：DOI 键或 URL 键任一匹配即算同文。
	topKeys := make(map[string]struct{})
	for _, b := range buckets {
		if len(b.Results) == 0 {
			continue
		}
		for _, k := range resultKeys(b.Results[0]) {
			topKeys[k] = struct{}{}
		}
	}
	for _, r := range scored {
		need := false
		for _, k := range resultKeys(r) {
			if _, ok := topKeys[k]; ok {
				need = true
				break
			}
		}
		if !need {
			continue
		}
		in := false
		for _, k := range resultKeys(r) {
			if _, ok := keptKeys[k]; ok {
				in = true
				break
			}
		}
		if !in {
			kept = append(kept, r)
			addKeys(r)
		}
	}

	if len(kept) > 1 {
		sort.SliceStable(kept, func(i, j int) bool {
			return kept[i].Score > kept[j].Score
		})
	}
	return kept
}
