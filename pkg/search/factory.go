package search

import (
	"strings"
	"websearch/pkg/antirobot"
	"websearch/pkg/baidu"
	"websearch/pkg/bing"
	"websearch/pkg/config"
	"websearch/pkg/ddg"
	"websearch/pkg/google"
	"websearch/pkg/log"
)

// SearchGroup 搜索引擎组，包含主引擎、兜底引擎和学术引擎。
type SearchGroup struct {
	Primary  SearchInf          // 主搜索引擎
	Fallback *BingSearchAdapter // Bing 兜底引擎（可为 nil）
	Academic AcademicSearcher   // 学术搜索引擎（可为 nil）
	conf     config.Config      // 保存配置，用于代理变更时重建引擎
}

// NewFromConfig 根据配置初始化搜索引擎组。
func NewFromConfig(conf config.Config) (*SearchGroup, error) {
	g := &SearchGroup{conf: conf}

	// ShowMeta 配置
	ShowMeta = conf.SmartSearch.ShowMeta

	// ── 初始化 Bing 引擎（兜底） ──
	initBingEngine(conf, g)

	// ── 初始化百度网页搜索引擎（无需 API Key，SK 失败时回退） ──
	baiduWebAdapter := initBaiduWebEngine(conf)

	// ── 初始化 Google 引擎（需代理，由 resolver 动态解析） ──
	googleAdapter := initGoogleEngine(conf)

	// ── 初始化 DuckDuckGo 引擎（需代理，由 resolver 动态解析） ──
	ddgAdapter := initDuckDuckGoEngine(conf)

	// ── 构建 KeyPool ──
	baiduPool := newKeyPoolFromList(conf.Baidu.EffectiveSKList(), "baidu")
	tavilyPool := newKeyPoolFromList(conf.Tavily.EffectiveSKList(), "tavily")
	exaPool := newKeyPoolFromList(conf.Exa.EffectiveSKList(), "exa")
	anysearchPool := newKeyPoolFromList(conf.Anysearch.EffectiveSKList(), "anysearch")

	// ── 按模式选择主引擎 ──
	switch conf.GetMode() {
	case config.ModeEngine:
		g.Primary = buildEngineMode(g, baiduWebAdapter, googleAdapter, ddgAdapter)
		log.Infof("搜索模式: engine（无需 API Key）")

	case config.ModeTavily:
		g.Primary = buildTavilyMode(tavilyPool, g, conf)

	case config.ModeExa:
		g.Primary = buildExaMode(exaPool, g, conf)

	case config.ModeAnysearch:
		g.Primary = buildAnysearchMode(anysearchPool, g, conf)

	case config.ModeApipool:
		g.Primary = buildApipoolMode(anysearchPool, baiduPool, tavilyPool, exaPool, baiduWebAdapter, g, conf)
		log.Infof("搜索模式: apipool（API Key 池轮转）")

	case config.ModeHybrid:
		g.Primary = buildHybridMode(anysearchPool, baiduPool, tavilyPool, exaPool, baiduWebAdapter, googleAdapter, ddgAdapter, g, conf)

	default: // baidu → 百度千帆 web_search
		g.Primary = buildBaiduMode(baiduPool, baiduWebAdapter, g, conf)
	}

	log.Infof("搜索模式: %s", conf.GetMode())

	// ── 学术搜索引擎（独立于主引擎） ──
	initAcademicEngine(conf, g)

	return g, nil
}

// ── 各模式构建函数 ──

// buildEngineMode 纯引擎模式：百度网页 + Bing + Google + DuckDuckGo 并发。
func buildEngineMode(g *SearchGroup, baiduWeb, google, ddg *EngineSearchAdapter) SearchInf {
	var engines []SearchInf
	if baiduWeb != nil {
		engines = append(engines, baiduWeb)
	}
	if g.Fallback != nil {
		engines = append(engines, g.Fallback)
	}
	if google != nil {
		engines = append(engines, google)
	}
	if ddg != nil {
		engines = append(engines, ddg)
	}
	if len(engines) == 0 {
		log.Error("engine 模式需要至少一个引擎，请检查 bing 配置")
		return nil
	}
	if len(engines) == 1 {
		return engines[0]
	}
	hs := NewHybridSearch(engines...)
	applySmartSearchFilters(hs, g.conf)
	return hs
}

// buildBaiduMode 百度千帆搜索（enable_ai_search 控制端点，失败自动回退百度网页搜索）。
func buildBaiduMode(pool *KeyPool, baiduWeb *EngineSearchAdapter, g *SearchGroup, conf config.Config) SearchInf {
	if pool != nil {
		primary := newBaiduSearchFromConf(pool, conf)
		if baiduWeb != nil {
			if conf.Baidu.EnableAISearch {
				log.Info("搜索模式: baidu（智能搜索 + 网页搜索回退）")
			} else {
				log.Info("搜索模式: baidu（千帆 SK + 网页搜索回退）")
			}
			return NewBaiduWithFallback(primary, baiduWeb)
		}
		return primary
	}
	if baiduWeb != nil {
		log.Info("搜索模式: baidu（网页搜索，无需 API Key）")
		return baiduWeb
	}
	if g.Fallback != nil {
		log.Error("mode=baidu 但未配置 baidu.api_key/sk_list 且无可用引擎，回退到 Bing")
		return g.Fallback
	}
	return nil
}

// buildTavilyMode Tavily 单引擎模式。
func buildTavilyMode(pool *KeyPool, g *SearchGroup, conf config.Config) SearchInf {
	if pool == nil {
		log.Error("mode=tavily 但未配置 tavily.api_key/sk_list，回退到 engine 模式")
		if g.Fallback != nil {
			return g.Fallback
		}
		return nil
	}
	return NewTavilySearch(pool, conf.BlackListHost)
}

// buildExaMode Exa 单引擎模式。
func buildExaMode(pool *KeyPool, g *SearchGroup, conf config.Config) SearchInf {
	if pool == nil {
		log.Error("mode=exa 但未配置 exa.api_key/sk_list，回退到 engine 模式")
		if g.Fallback != nil {
			return g.Fallback
		}
		return nil
	}
	numResults := conf.Exa.NumResults
	if numResults <= 0 {
		numResults = 5
	}
	lookbackDays := conf.Exa.LookbackDays
	if lookbackDays <= 0 {
		lookbackDays = 90
	}
	return NewExaSearchWithResults(pool, numResults, lookbackDays, conf.BlackListHost)
}

// buildAnysearchMode AnySearch 单引擎模式。
func buildAnysearchMode(pool *KeyPool, g *SearchGroup, conf config.Config) SearchInf {
	if pool == nil {
		log.Error("mode=anysearch 但未配置 anysearch.api_key/sk_list，回退到 engine 模式")
		if g.Fallback != nil {
			return g.Fallback
		}
		return nil
	}
	return NewAnysearchSearch(pool, conf.Anysearch.NumResults, conf.BlackListHost)
}

// buildApipoolMode API Key 池轮转模式：每次请求只调用一个供应商，失败自动切换下一个。
// 跨请求时供应商选择由策略决定（round-robin / priority / weighted），Key 均 round-robin 轮转。
// 供应商顺序由 conf.Apipool.Engines 配置控制（默认 anysearch → baidu → tavily → exa）。
// 百度端点由 baidu.enable_ai_search 配置控制（默认 true=智能搜索）。
func buildApipoolMode(anysearchPool, baiduPool, tavilyPool, exaPool *KeyPool, baiduWeb *EngineSearchAdapter, g *SearchGroup, conf config.Config) SearchInf {
	// 按配置顺序构建供应商（web 兜底始终追加在末尾）
	var providers []apipoolProvider
	for _, name := range conf.Apipool.GetEngines() {
		switch strings.ToLower(name) {
		case "anysearch":
			if anysearchPool != nil {
				providers = append(providers, apipoolProvider{name: "anysearch", engine: NewAnysearchSearch(anysearchPool, conf.Anysearch.NumResults, conf.BlackListHost), pool: anysearchPool})
			}
		case "baidu":
			if baiduPool != nil {
				providers = append(providers, apipoolProvider{name: "baidu", engine: newBaiduSearchFromConf(baiduPool, conf), pool: baiduPool})
			}
		case "tavily":
			if tavilyPool != nil {
				providers = append(providers, apipoolProvider{name: "tavily", engine: NewTavilySearch(tavilyPool, conf.BlackListHost), pool: tavilyPool})
			}
		case "exa":
			if exaPool != nil {
				numResults := conf.Exa.NumResults
				if numResults <= 0 {
					numResults = 5
				}
				lookbackDays := conf.Exa.LookbackDays
				if lookbackDays <= 0 {
					lookbackDays = 90
				}
				providers = append(providers, apipoolProvider{name: "exa", engine: NewExaSearchWithResults(exaPool, numResults, lookbackDays, conf.BlackListHost), pool: exaPool})
			}
		default:
			log.Infof("apipool: 未知供应商 %q，跳过", name)
		}
	}
	// 百度网页搜索作为最终兜底（无需 Key，始终在末尾）
	if baiduWeb != nil {
		providers = append(providers, apipoolProvider{name: "baidu_web", engine: baiduWeb, pool: nil})
	}
	if len(providers) == 0 {
		log.Error("apipool 模式需要至少配置一个 API Key（anysearch/baidu/tavily/exa）")
		return nil
	}
	ap := NewApipoolSearch(conf.Apipool.GetStrategy(), providers...)
	if conf.Apipool.GetStrategy() == "weighted" {
		ap.SetWeights(conf.Apipool.GetWeights())
	}
	if conf.SmartSearch.MaxSize > 0 {
		ap.SetMaxSize(conf.SmartSearch.MaxSize)
	}
	return ap
}

// buildHybridMode 全引擎混合模式：Anysearch + 百度搜索 + 百度网页搜索 + Tavily + Exa + Bing + Google + DuckDuckGo。
func buildHybridMode(anysearchPool, baiduPool, tavilyPool, exaPool *KeyPool, baiduWeb, google, ddg *EngineSearchAdapter, g *SearchGroup, conf config.Config) SearchInf {
	var engines []SearchInf
	if anysearchPool != nil {
		engines = append(engines, NewAnysearchSearch(anysearchPool, conf.Anysearch.NumResults, conf.BlackListHost))
	}
	if baiduPool != nil {
		engines = append(engines, newBaiduSearchFromConf(baiduPool, conf))
	}
	if baiduWeb != nil {
		engines = append(engines, baiduWeb)
	}
	if tavilyPool != nil {
		engines = append(engines, NewTavilySearch(tavilyPool, conf.BlackListHost))
	}
	if exaPool != nil {
		numResults := conf.Exa.NumResults
		if numResults <= 0 {
			numResults = 5
		}
		lookbackDays := conf.Exa.LookbackDays
		if lookbackDays <= 0 {
			lookbackDays = 90
		}
		engines = append(engines, NewExaSearchWithResults(exaPool, numResults, lookbackDays, conf.BlackListHost))
	}
	if g.Fallback != nil {
		engines = append(engines, g.Fallback)
	}
	if google != nil {
		engines = append(engines, google)
	}
	if ddg != nil {
		engines = append(engines, ddg)
	}
	if len(engines) == 0 {
		log.Error("hybrid 模式无可用搜索引擎")
		return nil
	}
	hs := NewHybridSearch(engines...)
	applySmartSearchFilters(hs, conf)
	return hs
}

// ── 辅助函数 ──

// newBaiduSearchFromConf 根据配置创建百度搜索实例（enable_ai_search 控制端点选择）。
func newBaiduSearchFromConf(pool *KeyPool, conf config.Config) SearchInf {
	if conf.Baidu.EnableAISearch {
		return NewBaiduAISearch(
			pool,
			conf.BlackListHost,
			conf.Baidu.Model,
			conf.Baidu.SearchSource,
			conf.Baidu.EnableReasoning,
			conf.Baidu.EnableDeepSearch,
			conf.Baidu.SearchMode,
		)
	}
	return NewBaiduSeach(pool, conf.BlackListHost)
}

// newKeyPoolFromList 从 key 列表创建 KeyPool，列表为空时返回 nil。
func newKeyPoolFromList(keys []string, name string) *KeyPool {
	if len(keys) == 0 {
		return nil
	}
	pool, err := NewKeyPool(keys)
	if err != nil {
		log.Errf("创建 %s KeyPool 失败: %v", name, err)
		return nil
	}
	if pool.Len() > 1 {
		log.Infof("%s KeyPool: %d 个 Key 轮询", name, pool.Len())
	}
	return pool
}

// initBaiduWebEngine 初始化百度网页搜索引擎。
func initBaiduWebEngine(conf config.Config) *EngineSearchAdapter {
	// 失效引擎默认禁用：实测 tn=json 直抓被百度 CAPTCHA 识别（2026-09-03，
	// 详见 config.BaiduConfig 注释），出口 IP 干净的环境可显式开启。
	if !conf.Baidu.WebEnabled {
		log.Info("百度网页搜索引擎已禁用（baidu.web_enabled=false，被反爬识别 CAPTCHA）")
		return nil
	}
	blocked := bing.MergeBlocked(conf.BlackListHost, nil)
	eng := baidu.NewBaiduWeb(baidu.BaiduOpts{
		Enabled: true,
		Blocked: blocked,
		PerSec:  conf.GetRateLimitPerSec(),
		PerMin:  conf.GetRateLimitPerMin(),
	})
	adapter := NewEngineSearchAdapter("baidu", eng)
	log.Info("百度网页搜索引擎已启用（tn=json，无需 API Key；注意：可能被反爬拦截）")
	return adapter
}

// initGoogleEngine 初始化 Google 引擎。
func initGoogleEngine(conf config.Config) *EngineSearchAdapter {
	if !conf.Google.Enabled {
		log.Info("Google 引擎已禁用（google.enabled=false，被反爬拦截暂不可用）")
		return nil
	}
	blocked := bing.MergeBlocked(conf.BlackListHost, conf.Google.Blocked)
	eng := google.NewGoogle(google.GoogleOpts{
		Enabled:      true,
		Blocked:      blocked,
		ProxyResolve: conf.Proxy.ProxyResolver(),
		PerSec:       conf.GetRateLimitPerSec(),
		PerMin:       conf.GetRateLimitPerMin(),
	})
	adapter := NewEngineSearchAdapter("google", eng)
	log.Info("Google 引擎已启用（注意：可能被反爬拦截）")
	return adapter
}

// initDuckDuckGoEngine 初始化 DuckDuckGo 引擎。
func initDuckDuckGoEngine(conf config.Config) *EngineSearchAdapter {
	if !conf.DuckDuckGo.Enabled {
		log.Info("DuckDuckGo 引擎已禁用（duckduckgo.enabled=false）")
		return nil
	}
	if conf.Proxy.ProxyResolver() == nil {
		log.Info("DuckDuckGo 引擎跳过（需要代理）")
		return nil
	}
	blocked := bing.MergeBlocked(conf.BlackListHost, conf.DuckDuckGo.Blocked)
	eng := ddg.NewDuckDuckGo(ddg.DuckDuckGoOpts{
		Enabled:      true,
		Blocked:      blocked,
		ProxyResolve: conf.Proxy.ProxyResolver(),
		PerSec:       conf.GetRateLimitPerSec(),
		PerMin:       conf.GetRateLimitPerMin(),
	})
	adapter := NewEngineSearchAdapter("duckduckgo", eng)
	log.Info("DuckDuckGo 引擎已启用（代理: 自动检测）")
	return adapter
}

// initBingEngine 根据配置初始化 Bing 引擎适配器。
func initBingEngine(conf config.Config, g *SearchGroup) {
	if !conf.Bing.Enabled {
		log.Info("Bing 引擎已禁用（bing.enabled=false）")
		return
	}

	bingOpts := bing.BingOpts{
		Enabled: true,
		Blocked: bing.MergeBlocked(conf.BlackListHost, conf.Bing.Blocked),
		PerSec:  conf.GetRateLimitPerSec(),
		PerMin:  conf.GetRateLimitPerMin(),
	}

	g.Fallback = NewBingSearchAdapter(bingOpts)
	log.Infof("Bing 引擎已启用，引擎: %v", g.Fallback.Engines())
}

// initAcademicEngine 根据配置初始化学术搜索引擎。
func initAcademicEngine(conf config.Config, g *SearchGroup) {
	acad := conf.Academic
	if !acad.Enabled {
		log.Info("学术引擎未启用（academic.enabled=false）")
		return
	}

	network := antirobot.RegionChina
	if conf.IsInternational() {
		network = antirobot.RegionInternational
	}

	acadConf := AcademicConfig{
		Network: network,
		Arxiv: antirobot.ArxivOpts{
			Enabled: !acad.DisableArxiv,
			PerSec:  conf.GetRateLimitPerSec(),
			PerMin:  conf.GetRateLimitPerMin(),
		},
		Crossref:        antirobot.CrossrefOpts{Enabled: !acad.DisableCrossref},
		OpenAlex:        antirobot.OpenAlexOpts{Enabled: !acad.DisableOpenAlex},
		SemanticScholar: antirobot.SemanticScholarOpts{Enabled: !acad.DisableSemanticScholar, APIKey: acad.SemanticScholarAPIKey},
		PubMed:          antirobot.PubMedOpts{Enabled: !acad.DisablePubMed},
		GoogleScholar:   antirobot.GoogleScholarOpts{Enabled: !acad.DisableGoogleScholar},
		EuropePMC:       antirobot.EuropePMCOpts{Enabled: !acad.DisableEuropePMC},
		DBLP:            antirobot.DBLPOpts{Enabled: !acad.DisableDBLP},
		DOAJ:            antirobot.DOAJOpts{Enabled: !acad.DisableDOAJ},
		ProxyResolve:    conf.Proxy.ProxyResolver(),
	}

	adapter := NewAcademicAdapter(acadConf)
	if adapter == nil {
		log.Info("无可用学术引擎")
		return
	}

	// 学术评分增强（默认开启，配置独立于 smartsearch，属 academicsearch 工具）
	adapter.SetEnhance(conf.Academic.Enhance, conf.Academic.Threshold)
	if conf.Academic.Enhance {
		log.Infof("学术评分增强已启用（阀值=%.3f）", conf.Academic.Threshold)
	}

	g.Academic = adapter
	engines := adapter.Engines()
	log.Infof("学术引擎已启用: %v", engines)
}

// applySmartSearchFilters 将 SmartSearchConfig 转换为 HybridSearchImpl 的过滤规则与评分增强开关。
func applySmartSearchFilters(hs *HybridSearchImpl, conf config.Config) {
	sc := conf.SmartSearch
	if sc.MaxSize > 0 {
		hs.SetMaxSize(sc.MaxSize)
	}
	// Wigolo 本地评分增强（默认启用）
	enhance := sc.Enhance == nil || *sc.Enhance
	hs.SetEnhance(enhance, sc.RelevanceThreshold)
	if enhance {
		log.Infof("Wigolo 评分增强已启用（阀值=%.3f）", sc.RelevanceThreshold)
	}
	// MMR 多样性重排（默认启用）
	hs.SetMMR(sc.MMR)
	if enhance && sc.MMR.Enabled {
		log.Infof("MMR 多样性重排已启用（λ=%.2f）", sc.MMR.Lambda)
	}
	if len(sc.Engines) == 0 {
		return
	}
	engineMap := make(map[string]engineFilter, len(sc.Engines))
	for name, ec := range sc.Engines {
		engineMap[name] = engineFilter{
			minScore: ec.MinScore,
			maxSize:  ec.MaxSize,
			weight:   ec.Weight,
		}
	}
	hs.SetFilters(engineMap)
}
