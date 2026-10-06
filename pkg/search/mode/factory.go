// Package mode 搜索模式构建：按 config 的 mode 组合 provider / adapter / hybrid
// 为 NewFromConfig（组合根）提供各搜索模式的 Primary 引擎与引擎初始化。
package mode

import (
	"strings"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/search/engine/baidu"
	"websearch/pkg/search/engine/bing"
	"websearch/pkg/search/engine/ddg"
	"websearch/pkg/search/engine/googlenews"
	"websearch/pkg/search/engine/google"
	"websearch/pkg/search/engine/so360"
	"websearch/pkg/search/engine/wikipedia"
	"websearch/pkg/log"
	"websearch/pkg/search/adapter"
	"websearch/pkg/search/apipool"
	"websearch/pkg/search/core"
	"websearch/pkg/search/hybrid"
	"websearch/pkg/search/provider"
)

// ── 各模式构建函数 ────────────────────────────────────────────────────────────

// EngineAdapters engine/hybrid 模式的通用引擎适配器集合（nil = 未启用）。
// 用命名结构体承载同类型适配器，避免长位置参数列表静默交换顺序。
type EngineAdapters struct {
	BaiduWeb  *adapter.EngineSearchAdapter
	So360     *adapter.EngineSearchAdapter
	Wikipedia *adapter.EngineSearchAdapter
	GNews     *adapter.EngineSearchAdapter
	Google    *adapter.EngineSearchAdapter
	DDG       *adapter.EngineSearchAdapter
}

// BuildEngineMode 纯引擎模式：百度网页 + 360 + Wikipedia + Google News + Bing + Google + DuckDuckGo 并发。
func BuildEngineMode(conf config.Config, fallback *adapter.BingSearchAdapter, ads EngineAdapters) core.SearchInf {
	var engines []core.SearchInf
	if ads.BaiduWeb != nil {
		engines = append(engines, ads.BaiduWeb)
	}
	if ads.So360 != nil {
		engines = append(engines, ads.So360)
	}
	if ads.Wikipedia != nil {
		engines = append(engines, ads.Wikipedia)
	}
	if ads.GNews != nil {
		engines = append(engines, ads.GNews)
	}
	if fallback != nil {
		engines = append(engines, fallback)
	}
	if ads.Google != nil {
		engines = append(engines, ads.Google)
	}
	if ads.DDG != nil {
		engines = append(engines, ads.DDG)
	}
	if len(engines) == 0 {
		log.Error("engine 模式需要至少一个引擎，请检查 bing 配置")
		return nil
	}
	if len(engines) == 1 {
		return engines[0]
	}
	hs := hybrid.NewHybridSearch(engines...)
	applySmartSearchFilters(hs, conf)
	return hs
}

// BuildBaiduMode 百度千帆搜索（enable_ai_search 控制端点，失败自动回退百度网页搜索）。
func BuildBaiduMode(conf config.Config, pool *provider.KeyPool, baiduWeb *adapter.EngineSearchAdapter, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if pool != nil {
		primary := newBaiduSearchFromConf(pool, conf)
		if baiduWeb != nil {
			if conf.Baidu.EnableAISearch {
				log.Info("搜索模式: baidu（智能搜索 + 网页搜索回退）")
			} else {
				log.Info("搜索模式: baidu（千帆 SK + 网页搜索回退）")
			}
			return adapter.NewBaiduWithFallback(primary, baiduWeb)
		}
		return primary
	}
	if baiduWeb != nil {
		log.Info("搜索模式: baidu（网页搜索，无需 API Key）")
		return baiduWeb
	}
	if fallback != nil {
		log.Error("mode=baidu 但未配置 baidu.api_key/sk_list 且无可用引擎，回退到 Bing")
		return fallback
	}
	return nil
}

// tavilyOptions 根据 config 组装 Tavily 可选参数（include_raw_content 默认开）。
func tavilyOptions(conf config.Config) []provider.TavilyOption {
	if conf.Tavily.IncludeRawContent != nil && !*conf.Tavily.IncludeRawContent {
		return nil
	}
	return []provider.TavilyOption{provider.WithRawContent(true)}
}

// exaOptions 根据 config 组装 Exa 可选参数（contents.text 默认开）。
func exaOptions(conf config.Config) []provider.ExaOption {
	if conf.Exa.IncludeText != nil && !*conf.Exa.IncludeText {
		return nil
	}
	return []provider.ExaOption{provider.WithTextContents(conf.Exa.TextMaxCharacters)}
}

// BuildTavilyMode Tavily 单引擎模式。
func BuildTavilyMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if pool == nil {
		log.Error("mode=tavily 但未配置 tavily.api_key/sk_list，回退到 engine 模式")
		return fallback
	}
	return provider.NewTavilySearch(pool, conf.BlackListHost, tavilyOptions(conf)...)
}

// BuildExaMode Exa 单引擎模式。
func BuildExaMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if pool == nil {
		log.Error("mode=exa 但未配置 exa.api_key/sk_list，回退到 engine 模式")
		return fallback
	}
	numResults := conf.Exa.NumResults
	if numResults <= 0 {
		numResults = 5
	}
	lookbackDays := conf.Exa.LookbackDays
	if lookbackDays <= 0 {
		lookbackDays = 90
	}
	return provider.NewExaSearchWithResults(pool, numResults, lookbackDays, conf.BlackListHost, exaOptions(conf)...)
}

// BuildAnysearchMode AnySearch 单引擎模式。
func BuildAnysearchMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if pool == nil {
		// 零 Key 匿名档（P0-2 来源扩充）：上游支持免鉴权匿名 IP 限流档，
		// 本项目出口实测可用（2026-10-06）；匿名档受 provider 内保守限流钳制
		log.Info("mode=anysearch 未配置 Key，启用匿名档（免鉴权，按出口 IP 限流）")
		return provider.NewAnysearchSearch(nil, conf.Anysearch.NumResults, conf.BlackListHost)
	}
	return provider.NewAnysearchSearch(pool, conf.Anysearch.NumResults, conf.BlackListHost)
}

// BuildDoubaoMode 豆包联网搜索单引擎模式。
func BuildDoubaoMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if pool == nil {
		log.Error("mode=doubao 但未配置 doubao.api_key/sk_list，回退到 engine 模式")
		return fallback
	}
	return newDoubaoFromConf(pool, conf)
}

// BuildApipoolMode API Key 池轮转模式：每次请求只调用一个供应商，失败自动切换下一个。
// 跨请求时供应商选择由策略决定（round-robin / priority / weighted），Key 均 round-robin 轮转。
// 供应商顺序由 conf.Apipool.Engines 配置控制（默认 anysearch → baidu → tavily → exa）。
// 百度端点由 baidu.enable_ai_search 配置控制（默认 true=智能搜索）。
func BuildApipoolMode(conf config.Config, anysearchPool, baiduPool, tavilyPool, exaPool, doubaoPool *provider.KeyPool, baiduWeb *adapter.EngineSearchAdapter) core.SearchInf {
	// 按配置顺序构建供应商（web 兜底始终追加在末尾）
	var providers []apipool.ApipoolProvider
	for _, name := range conf.Apipool.GetEngines() {
		switch strings.ToLower(name) {
		case "anysearch":
			if anysearchPool != nil {
				providers = append(providers, apipool.NewApipoolProvider("anysearch", provider.NewAnysearchSearch(anysearchPool, conf.Anysearch.NumResults, conf.BlackListHost), anysearchPool))
			} else {
				// 零 Key 匿名档（P0-2）：无 SK 轮转，失败直接切换下一供应商
				providers = append(providers, apipool.NewApipoolProvider("anysearch", provider.NewAnysearchSearch(nil, conf.Anysearch.NumResults, conf.BlackListHost), nil))
			}
		case "baidu":
			if baiduPool != nil {
				providers = append(providers, apipool.NewApipoolProvider("baidu", newBaiduSearchFromConf(baiduPool, conf), baiduPool))
			}
		case "tavily":
			if tavilyPool != nil {
				providers = append(providers, apipool.NewApipoolProvider("tavily", provider.NewTavilySearch(tavilyPool, conf.BlackListHost, tavilyOptions(conf)...), tavilyPool))
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
				providers = append(providers, apipool.NewApipoolProvider("exa", provider.NewExaSearchWithResults(exaPool, numResults, lookbackDays, conf.BlackListHost, exaOptions(conf)...), exaPool))
			}
		case "doubao":
			if doubaoPool != nil {
				providers = append(providers, apipool.NewApipoolProvider("doubao", newDoubaoFromConf(doubaoPool, conf), doubaoPool))
			}
		default:
			log.Infof("apipool: 未知供应商 %q，跳过", name)
		}
	}
	// 百度网页搜索作为最终兜底（无需 Key，始终在末尾）
	if baiduWeb != nil {
		providers = append(providers, apipool.NewApipoolProvider("baidu_web", baiduWeb, nil))
	}
	if len(providers) == 0 {
		log.Error("apipool 模式需要至少一个 API Key（anysearch/baidu/tavily/exa）")
		return nil
	}
	ap := apipool.NewApipoolSearch(conf.Apipool.GetStrategy(), providers...)
	if conf.Apipool.GetStrategy() == "weighted" {
		ap.SetWeights(conf.Apipool.GetWeights())
	}
	if conf.SmartSearch.MaxSize > 0 {
		ap.SetMaxSize(conf.SmartSearch.MaxSize)
	}
	return ap
}

// BuildHybridMode 全引擎混合模式：Anysearch（有 Key 走轮转 / 无 Key 匿名档）+ 百度搜索 + 百度网页 + 360 + Wikipedia + Google News + Tavily + Exa + 豆包（有 Key 时）+ Bing + Google + DuckDuckGo。
func BuildHybridMode(conf config.Config, anysearchPool, baiduPool, tavilyPool, exaPool, doubaoPool *provider.KeyPool, ads EngineAdapters, fallback *adapter.BingSearchAdapter) core.SearchInf {
	var engines []core.SearchInf
	if anysearchPool != nil {
		engines = append(engines, provider.NewAnysearchSearch(anysearchPool, conf.Anysearch.NumResults, conf.BlackListHost))
	} else {
		// 零 Key 匿名档（P0-2）：无 Key 时也以匿名档加入混合编排（受保守限流钳制）
		engines = append(engines, provider.NewAnysearchSearch(nil, conf.Anysearch.NumResults, conf.BlackListHost))
	}
	if baiduPool != nil {
		engines = append(engines, newBaiduSearchFromConf(baiduPool, conf))
	}
	if ads.BaiduWeb != nil {
		engines = append(engines, ads.BaiduWeb)
	}
	if ads.So360 != nil {
		engines = append(engines, ads.So360)
	}
	if ads.Wikipedia != nil {
		engines = append(engines, ads.Wikipedia)
	}
	if ads.GNews != nil {
		engines = append(engines, ads.GNews)
	}
	if tavilyPool != nil {
		engines = append(engines, provider.NewTavilySearch(tavilyPool, conf.BlackListHost, tavilyOptions(conf)...))
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
		engines = append(engines, provider.NewExaSearchWithResults(exaPool, numResults, lookbackDays, conf.BlackListHost, exaOptions(conf)...))
	}
	if doubaoPool != nil {
		engines = append(engines, newDoubaoFromConf(doubaoPool, conf))
	}
	if fallback != nil {
		engines = append(engines, fallback)
	}
	if ads.Google != nil {
		engines = append(engines, ads.Google)
	}
	if ads.DDG != nil {
		engines = append(engines, ads.DDG)
	}
	if len(engines) == 0 {
		log.Error("hybrid 模式无可用搜索引擎")
		return nil
	}
	hs := hybrid.NewHybridSearch(engines...)
	applySmartSearchFilters(hs, conf)
	return hs
}

// ── 引擎初始化 ────────────────────────────────────────────────────────────────

// InitBaiduWebEngine 初始化百度网页搜索引擎。
func InitBaiduWebEngine(conf config.Config) *adapter.EngineSearchAdapter {
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
	a := adapter.NewEngineSearchAdapter("baidu", eng)
	log.Info("百度网页搜索引擎已启用（tn=json，无需 API Key；注意：可能被反爬拦截）")
	return a
}

// InitGoogleEngine 初始化 Google 引擎。
func InitGoogleEngine(conf config.Config) *adapter.EngineSearchAdapter {
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
	a := adapter.NewEngineSearchAdapter("google", eng)
	log.Info("Google 引擎已启用（注意：可能被反爬拦截）")
	return a
}

// InitDuckDuckGoEngine 初始化 DuckDuckGo 引擎。
func InitDuckDuckGoEngine(conf config.Config) *adapter.EngineSearchAdapter {
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
	a := adapter.NewEngineSearchAdapter("duckduckgo", eng)
	log.Info("DuckDuckGo 引擎已启用（代理: 自动检测）")
	return a
}

// InitSo360Engine 初始化 360 搜索引擎（国内直连可用，零 Key）。
func InitSo360Engine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.So360.Enabled {
		return nil
	}
	blocked := bing.MergeBlocked(conf.BlackListHost, conf.So360.Blocked)
	eng := so360.NewSo360(so360.So360Opts{
		Enabled:    true,
		Blocked:    blocked,
		PerSec:     conf.GetRateLimitPerSec(),
		PerMin:     conf.GetRateLimitPerMin(),
		SafeSearch: conf.So360.SafeSearch,
	})
	a := adapter.NewEngineSearchAdapter("so360", eng)
	log.Info("360 搜索引擎已启用（直连可用，零 Key）")
	return a
}

// InitWikipediaEngine 初始化维基百科引擎（MediaWiki API，零 Key；国内出口需代理）。
func InitWikipediaEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.Wikipedia.Enabled {
		return nil
	}
	if conf.Proxy.ProxyResolver() == nil {
		log.Info("Wikipedia 引擎跳过（需代理）")
		return nil
	}
	eng := wikipedia.NewWikipedia(wikipedia.WikipediaOpts{
		Enabled:      true,
		Lang:         conf.Wikipedia.Lang,
		NumResults:   conf.Wikipedia.NumResults,
		PerSec:       conf.Wikipedia.PerSec,
		PerMin:       conf.Wikipedia.PerMin,
		ProxyResolve: conf.Proxy.ProxyResolver(),
	})
	a := adapter.NewEngineSearchAdapter("wikipedia", eng)
	log.Info("Wikipedia 引擎已启用（代理: 自动检测）")
	return a
}

// InitGoogleNewsEngine 初始化 Google News RSS 引擎（零 Key；国内出口需代理）。
func InitGoogleNewsEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.GoogleNews.Enabled {
		return nil
	}
	if conf.Proxy.ProxyResolver() == nil {
		log.Info("Google News 引擎跳过（需代理）")
		return nil
	}
	eng := googlenews.NewGoogleNews(googlenews.GoogleNewsOpts{
		Enabled:      true,
		Edition:      conf.GoogleNews.Edition,
		NumResults:   conf.GoogleNews.NumResults,
		PerSec:       conf.GoogleNews.PerSec,
		PerMin:       conf.GoogleNews.PerMin,
		ProxyResolve: conf.Proxy.ProxyResolver(),
	})
	a := adapter.NewEngineSearchAdapter("googlenews", eng)
	log.Info("Google News 引擎已启用（代理: 自动检测；跳转链接自动回源发布方 URL）")
	return a
}

// InitBingEngine 根据配置初始化 Bing 引擎适配器（各模式通用兜底）。
func InitBingEngine(conf config.Config) *adapter.BingSearchAdapter {
	if !conf.Bing.Enabled {
		log.Info("Bing 引擎已禁用（bing.enabled=false）")
		return nil
	}

	bingOpts := bing.BingOpts{
		Enabled: true,
		Blocked: bing.MergeBlocked(conf.BlackListHost, conf.Bing.Blocked),
		PerSec:  conf.GetRateLimitPerSec(),
		PerMin:  conf.GetRateLimitPerMin(),
	}

	fallback := adapter.NewBingSearchAdapter(bingOpts)
	log.Infof("Bing 引擎已启用，引擎: %v", fallback.Engines())
	return fallback
}

// InitAcademicEngine 根据配置初始化学术搜索引擎。
func InitAcademicEngine(conf config.Config) core.AcademicSearcher {
	acad := conf.Academic
	if !acad.Enabled {
		log.Info("学术引擎未启用（academic.enabled=false）")
		return nil
	}

	network := antirobot.RegionChina
	if conf.IsInternational() {
		network = antirobot.RegionInternational
	}

	acadConf := adapter.AcademicConfig{
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
		UnpaywallEmail:  acad.UnpaywallEmail,
	}

	a := adapter.NewAcademicAdapter(acadConf)
	if a == nil {
		log.Info("无可用学术引擎")
		return nil
	}

	// 学术评分增强（默认开启，配置独立于 smartsearch，属 academicsearch 工具）
	a.SetEnhance(conf.Academic.Enhance, conf.Academic.Threshold)
	if conf.Academic.Enhance {
		log.Infof("学术评分增强已启用（阀值=%.3f）", conf.Academic.Threshold)
	}

	log.Infof("学术引擎已启用: %v", a.Engines())
	return a
}

// ── 供应商构建辅助 ────────────────────────────────────────────────────────────

// newBaiduSearchFromConf 根据配置创建百度搜索实例（enable_ai_search 控制端点选择）。
func newBaiduSearchFromConf(pool *provider.KeyPool, conf config.Config) core.SearchInf {
	if conf.Baidu.EnableAISearch {
		return provider.NewBaiduAISearch(
			pool,
			conf.BlackListHost,
			conf.Baidu.Model,
			conf.Baidu.SearchSource,
			conf.Baidu.EnableReasoning,
			conf.Baidu.EnableDeepSearch,
			conf.Baidu.SearchMode,
		)
	}
	return provider.NewBaiduSeach(pool, conf.BlackListHost)
}

func newDoubaoFromConf(pool *provider.KeyPool, conf config.Config) *provider.DoubaoSearchImpl {
	return provider.NewDoubaoSearch(pool, provider.DoubaoOptions{
		NumResults:          conf.Doubao.NumResults,
		ExcludeDomains:      conf.BlackListHost,
		Version:             conf.Doubao.GetVersion(),
		TimeRange:           conf.Doubao.TimeRange,
		AuthLevel:           conf.Doubao.AuthLevel,
		QueryRewrite:        conf.Doubao.QueryRewrite,
		NeedContent:         conf.Doubao.NeedContent,
		MaxSnippetLength:    conf.Doubao.MaxSnippetLength,
		MaxImageCountPerDoc: conf.Doubao.MaxImageCountPerDoc,
		ICPHostOnly:         conf.Doubao.ICPHostOnly,
	})
}

// applySmartSearchFilters 将 SmartSearchConfig 转换为 hybrid.HybridSearchImpl 的过滤规则与评分增强开关。
func applySmartSearchFilters(hs *hybrid.HybridSearchImpl, conf config.Config) {
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
	// off-topic 整桶守卫（P1-7 回查处置：配置省略 = shadow 只记录不丢弃，enforce 须显式开启）
	guardMode := sc.OffTopicGuardMode()
	hs.SetOffTopicGuard(guardMode)
	log.Infof("off-topic 整桶守卫模式: %s", guardMode)
	if len(sc.Engines) == 0 {
		return
	}
	engineMap := make(map[string]hybrid.EngineFilter, len(sc.Engines))
	for name, ec := range sc.Engines {
		engineMap[name] = hybrid.EngineFilter{
			MinScore: ec.MinScore,
			MaxSize:  ec.MaxSize,
			Weight:   ec.Weight,
		}
	}
	hs.SetFilters(engineMap)
}
