package adapter

import (
	"websearch/pkg/search/enhance"
	"websearch/pkg/search/core"

	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"websearch/pkg/academic"
	"websearch/pkg/antirobot"
	"websearch/pkg/log"
	"websearch/pkg/proxy"
)

// ──────────────────────────────────────────────────────────────────────────────
// AcademicAdapter 学术搜索引擎适配器
// ──────────────────────────────────────────────────────────────────────────────

// AcademicAdapter 将学术引擎适配为 core.AcademicSearcher 接口。
// 负责 arXiv、Crossref、OpenAlex、Semantic Scholar、PubMed、Google Scholar。
type AcademicAdapter struct {
	searcher       *antirobot.Searcher
	engines        []antirobot.Engine // 保存全部引擎引用，用于按名过滤
	enhance        bool              // 是否启用学术评分增强（RRF 融合 + 学术信号）
	threshold      float64           // 学术结果阀值（默认 0.02）
	unpaywallEmail string
}


// SetEnhance 启用/关闭学术评分增强，threshold <= 0 时使用默认 0.02。
func (a *AcademicAdapter) SetEnhance(enabled bool, threshold float64) {
	a.enhance = enabled
	a.threshold = threshold
}

// AcademicConfig 学术引擎配置。
type AcademicConfig struct {
	Network         antirobot.NetworkRegion
	Arxiv           antirobot.ArxivOpts
	Crossref        antirobot.CrossrefOpts
	OpenAlex        antirobot.OpenAlexOpts
	SemanticScholar antirobot.SemanticScholarOpts
	PubMed          antirobot.PubMedOpts
	GoogleScholar   antirobot.GoogleScholarOpts
	EuropePMC       antirobot.EuropePMCOpts
	DBLP            antirobot.DBLPOpts
	DOAJ            antirobot.DOAJOpts
	ProxyResolve    proxy.ProxyResolver // 代理端点动态解析函数
	UnpaywallEmail  string
}

// NewAcademicAdapter 创建学术搜索适配器。
func NewAcademicAdapter(conf AcademicConfig) *AcademicAdapter {
	engines := academic.BuildAcademic(struct {
		Network         antirobot.NetworkRegion
		Arxiv           antirobot.ArxivOpts
		Crossref        antirobot.CrossrefOpts
		OpenAlex        antirobot.OpenAlexOpts
		SemanticScholar antirobot.SemanticScholarOpts
		PubMed          antirobot.PubMedOpts
		GoogleScholar   antirobot.GoogleScholarOpts
		EuropePMC       antirobot.EuropePMCOpts
		DBLP            antirobot.DBLPOpts
		DOAJ            antirobot.DOAJOpts
		ProxyResolve    proxy.ProxyResolver
	}{
		Network:         conf.Network,
		Arxiv:           conf.Arxiv,
		Crossref:        conf.Crossref,
		OpenAlex:        conf.OpenAlex,
		SemanticScholar: conf.SemanticScholar,
		PubMed:          conf.PubMed,
		GoogleScholar:   conf.GoogleScholar,
		EuropePMC:       conf.EuropePMC,
		DBLP:            conf.DBLP,
		DOAJ:            conf.DOAJ,
		ProxyResolve:    conf.ProxyResolve,
	})

	if len(engines) == 0 {
		return nil
	}

	searcher := antirobot.NewSearcher(antirobot.StrategyParallel, engines)
	return &AcademicAdapter{searcher: searcher, engines: engines, unpaywallEmail: conf.UnpaywallEmail}
}

// SearchAcademicRaw 实现 core.AcademicSearcher 接口，返回学术论文搜索结果。
// 单个引擎失败不影响整体：失败引擎记入 core.AcademicSearchResult.EngineErrors，
// 调用方据此提示结果可能不完整。
func (a *AcademicAdapter) SearchAcademicRaw(query string, opts ...core.AcademicSearchOptions) (core.AcademicSearchResult, error) {
	var opt core.AcademicSearchOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Page <= 0 {
		opt.Page = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if kind, id := academic.ParsePaperQuery(query); kind != "" {
		return a.lookupPaper(ctx, kind, id)
	}

	// 设置时间范围
	tr := core.ParseTimeRange(opt.TimeRange)
	a.searcher.TimeRange = tr

	// 按引擎名过滤
	searcher := a.searcher
	if len(opt.Engines) > 0 {
		filtered := a.filterEngines(opt.Engines)
		if len(filtered) == 0 {
			return core.AcademicSearchResult{}, fmt.Errorf("指定的引擎均不可用: %v", opt.Engines)
		}
		searcher = antirobot.NewSearcher(antirobot.StrategyParallel, filtered)
		searcher.TimeRange = tr
	}

	responses := searcher.Search(ctx, query, opt.Page)

	// 保留 per-engine 排名信息（评分增强的 RRF 融合依赖各引擎内部顺序）
	var engineErrors map[string]string
	var buckets []core.ScoreBucket
	var all []antirobot.Result
	for _, resp := range responses {
		if resp.Error != "" {
			log.Warnf("academic: engine %s failed: %v", resp.Engine, resp.Error)
			if engineErrors == nil {
				engineErrors = make(map[string]string)
			}
			engineErrors[resp.Engine] = resp.Error
			continue
		}
		if a.enhance {
			results := resp.Results
			// 引擎回传 score（OpenAlex / Semantic Scholar）时按 score 降序作为排名
			hasScore := false
			for _, r := range results {
				if r.Score > 0 {
					hasScore = true
					break
				}
			}
			if hasScore {
				sort.SliceStable(results, func(i, j int) bool {
					return results[i].Score > results[j].Score
				})
			}
			buckets = append(buckets, core.ScoreBucket{Name: resp.Engine, Results: toSearchResults(results)})
		}
		all = append(all, resp.Results...)
	}

	// 学术评分增强：RRF 融合 + 引用数/期刊/PDF/新鲜度信号 + 阀值过滤
	if a.enhance {
		results := enhance.EnhanceAcademicResults(query, buckets, a.threshold, 0)
		if len(results) == 0 {
			return core.AcademicSearchResult{}, noResultError(engineErrors)
		}
		a.enrichOAPDF(ctx, results)
		return core.AcademicSearchResult{Results: results, EngineErrors: engineErrors}, nil
	}

	all = antirobot.DeduplicateResults(all)
	all = antirobot.NormalizeAndSortResults(all)

	if len(all) == 0 {
		return core.AcademicSearchResult{}, noResultError(engineErrors)
	}

	results := toSearchResults(all)
	a.enrichOAPDF(ctx, results)
	return core.AcademicSearchResult{Results: results, EngineErrors: engineErrors}, nil
}

func (a *AcademicAdapter) lookupPaper(ctx context.Context, kind, id string) (core.AcademicSearchResult, error) {
	var raw []antirobot.Result
	switch kind {
	case "doi":
		raw = academic.LookupDOI(ctx, id)
	case "arxiv":
		raw = academic.LookupArxivID(ctx, id)
	}
	if len(raw) == 0 {
		return core.AcademicSearchResult{}, fmt.Errorf("学术引擎搜索无结果")
	}
	raw = antirobot.DeduplicateResults(raw)
	results := toSearchResults(raw)
	if len(results) > 1 {
		results = results[:1]
	}
	a.enrichOAPDF(ctx, results)
	return core.AcademicSearchResult{Results: results}, nil
}

func (a *AcademicAdapter) enrichOAPDF(ctx context.Context, results []core.SearchResult) {
	if a.unpaywallEmail == "" || len(results) == 0 {
		return
	}
	var wg sync.WaitGroup
	for i := range results {
		if results[i].DOI == "" || results[i].PDFURL != "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pdf, err := academic.UnpaywallPDF(ctx, results[i].DOI, a.unpaywallEmail)
			if err == nil && pdf != "" {
				results[i].PDFURL = pdf
			}
		}(i)
	}
	wg.Wait()
}

// noResultError 全部引擎无结果时返回错误；有引擎失败的把错误拼进 message。
func noResultError(engineErrors map[string]string) error {
	if len(engineErrors) == 0 {
		return fmt.Errorf("学术引擎搜索无结果")
	}
	return fmt.Errorf("学术引擎搜索无结果（%s）", engineErrorSummary(engineErrors))
}

// engineErrorSummary 把逐引擎错误拼成单行摘要（引擎名稳定排序）。
func engineErrorSummary(errs map[string]string) string {
	names := slices.Sorted(maps.Keys(errs))
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s: %s", n, errs[n]))
	}
	return strings.Join(parts, "; ")
}

// toSearchResults 将 antirobot.Result 列表转换为 core.SearchResult 列表（保留学术元数据与 score）。
func toSearchResults(all []antirobot.Result) []core.SearchResult {
	results := make([]core.SearchResult, 0, len(all))
	for _, r := range all {
		results = append(results, core.SearchResult{
			Title:       r.Title,
			Url:         strings.TrimSpace(r.URL),
			Content:     r.Content,
			PublishDate: r.PublishedAt,
			DateSource:  r.DateSource,
			Type:        string(r.Type),
			Authors:     r.Authors,
			DOI:         r.DOI,
			Journal:     r.Journal,
			CitedBy:     r.CitedBy,
			PDFURL:      r.PDFURL,
			Score:       r.Score,
		})
	}
	return results
}

// MergeContent 格式化学术搜索结果为 Markdown。
func (a *AcademicAdapter) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if val.Type == "paper" {
			citedByStr := ""
			if val.CitedBy > 0 {
				citedByStr = strconv.Itoa(val.CitedBy)
			}
			buf.WriteString(core.FormatPaperMD(i+1, val.Title, val.Url,
				val.Authors, val.DOI, val.Journal, val.PublishDate,
				val.PDFURL, citedByStr, val.Content))
		} else {
			buf.WriteString(core.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}

// MergeContentWithErrors 格式化学术搜索结果，末尾附逐引擎失败警告。
// engineErrors 为空时输出与 MergeContent 完全一致。
func (a *AcademicAdapter) MergeContentWithErrors(query string, results []core.SearchResult, engineErrors map[string]string) (string, error) {
	out, err := a.MergeContent(query, results)
	if err != nil {
		return "", err
	}
	if len(engineErrors) == 0 {
		return out, nil
	}
	var b strings.Builder
	b.WriteString(out)
	b.WriteString("\n> ⚠ 部分引擎本次失败，结果可能不完整：")
	for _, n := range slices.Sorted(maps.Keys(engineErrors)) {
		b.WriteString(fmt.Sprintf("%s (%s)；", n, engineErrors[n]))
	}
	return b.String(), nil
}

// Engines 返回已注册的学术引擎名称列表。
func (a *AcademicAdapter) Engines() []string {
	return a.searcher.Engines()
}

// AcademicEngines 实现 core.AcademicSearcher 接口。
func (a *AcademicAdapter) AcademicEngines() []string {
	return a.searcher.Engines()
}

// filterEngines 按名称过滤引擎子集。
func (a *AcademicAdapter) filterEngines(names []string) []antirobot.Engine {
	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		nameSet[strings.ToLower(strings.TrimSpace(n))] = struct{}{}
	}
	var filtered []antirobot.Engine
	for _, eng := range a.engines {
		if _, ok := nameSet[eng.Name()]; ok {
			filtered = append(filtered, eng)
		}
	}
	return filtered
}
