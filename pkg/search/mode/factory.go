// Package mode 搜索模式构建：按 config 的 mode 组合 provider / adapter / hybrid
// 为 NewFromConfig（组合根）提供各搜索模式的 Primary 引擎与引擎初始化。
package mode

import (
	"errors"
	"strings"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
	"websearch/pkg/search/adapter"
	"websearch/pkg/search/apipool"
	"websearch/pkg/search/core"
	"websearch/pkg/search/engine/baidu"
	"websearch/pkg/search/engine/bing"
	"websearch/pkg/search/engine/ddg"
	"websearch/pkg/search/engine/github"
	"websearch/pkg/search/engine/google"
	"websearch/pkg/search/engine/googlenews"
	"websearch/pkg/search/engine/mojeek"
	"websearch/pkg/search/engine/so360"
	"websearch/pkg/search/engine/sogou"
	"websearch/pkg/search/engine/wikipedia"
	"websearch/pkg/search/hybrid"
	"websearch/pkg/search/provider"
	"websearch/pkg/search/provider/mcphub"
)

// ── 各模式构建函数 ────────────────────────────────────────────────────────────

// EngineAdapters engine/hybrid 模式的通用引擎适配器集合（nil = 未启用）。
// 用命名结构体承载同类型适配器，避免长位置参数列表静默交换顺序。
type EngineAdapters struct {
	BaiduWeb  *adapter.EngineSearchAdapter
	So360     *adapter.EngineSearchAdapter
	Sogou     *adapter.EngineSearchAdapter
	Mojeek    *adapter.EngineSearchAdapter
	Wikipedia *adapter.EngineSearchAdapter
	GNews     *adapter.EngineSearchAdapter
	GitHub    core.SearchInf // GitHubWithFallback（API 失败回退 Bing 站点限定检索）
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
	if ads.Sogou != nil {
		engines = append(engines, ads.Sogou)
	}
	if ads.Mojeek != nil {
		engines = append(engines, ads.Mojeek)
	}
	if ads.Wikipedia != nil {
		engines = append(engines, ads.Wikipedia)
	}
	if ads.GNews != nil {
		engines = append(engines, ads.GNews)
	}
	if ads.GitHub != nil {
		engines = append(engines, ads.GitHub)
	}
	if conf.Bing.Enabled && fallback != nil {
		engines = append(engines, fallback)
	}
	if ads.Google != nil {
		engines = append(engines, ads.Google)
	}
	if ads.DDG != nil {
		engines = append(engines, ads.DDG)
	}
	if len(engines) == 0 {
		log.ErrorT("log.search.mode.engine_mode_requires_at_least_one")
		return nil
	}
	if len(engines) == 1 {
		return engines[0]
	}
	hs := hybrid.NewHybridSearch(engines...)
	applySmartSearchFilters(hs, conf)
	return hs
}

// BuildMCPHubMode 零 Key 托管 MCP 级联模式：按配置顺序轮转
// Parallel / AnySearch / Exa / Firecrawl，失败冷却后自动切下一个；
// 可把本地网页引擎加入轮转池分摊匿名限流，全部失败后走 fallback_engines，
// 最终由 smartsearch 统一回退 Bing。
func BuildMCPHubMode(conf config.Config, ads EngineAdapters, fallback *adapter.BingSearchAdapter) core.SearchInf {
	engines := make(map[string]core.SearchInf, 7)
	if ads.BaiduWeb != nil {
		engines["baidu_web"] = ads.BaiduWeb
	}
	if ads.So360 != nil {
		engines["so360"] = ads.So360
	}
	if ads.Sogou != nil {
		engines["sogou"] = ads.Sogou
	}
	if ads.Mojeek != nil {
		engines["mojeek"] = ads.Mojeek
	}
	if ads.Wikipedia != nil {
		engines["wikipedia"] = ads.Wikipedia
	}
	if ads.GNews != nil {
		engines["googlenews"] = ads.GNews
	}
	if ads.GitHub != nil {
		engines["github"] = ads.GitHub
	}
	if ads.Google != nil {
		engines["google"] = ads.Google
	}
	if ads.DDG != nil {
		engines["duckduckgo"] = ads.DDG
	}
	if fallback != nil {
		engines["bing"] = fallback
	}
	hub, err := mcphub.New(conf.MCPHub, conf.Proxy.ProxyResolver(), engines)
	if err != nil {
		log.ErrorT("log.search.mode.mode_mcphub_initialization_failed", "err", err)
		return nil
	}
	fallbacks := hub.FallbackNames()
	log.InfoT("log.search.mode.search_mode_mcphub_rotation_fallback_final", "join", strings.Join(hub.RotationNames(), " → "), "join2", strings.Join(fallbacks, " → "))
	return hub
}

// buildApipoolMCPHub 构建 apipool 后备补充源使用的托管 MCP 级联。
// 本地网页引擎不重复注入：apipool 里 baidu_web 已作为独立供应商兜底，
// 这里只让托管 server 之间级联与冷却。
//
// taken 是 apipool 已装好的供应商标识；同名托管 server 会被跳过，避免同一
// 来源（如 anysearch / exa）在同一进程里被两条链路重复请求、叠加 IP 限流。
func buildApipoolMCPHub(conf config.Config, taken map[string]bool) (core.SearchInf, error) {
	hubCfg := conf.MCPHub
	if conf.Apipool.DedupeMCPHubSources() && len(taken) > 0 {
		servers := hubCfg.GetServers()
		kept := make([]config.MCPHubServerConfig, 0, len(servers))
		for _, s := range servers {
			name := strings.ToLower(strings.TrimSpace(s.Name))
			if taken[name] {
				log.InfoT("log.search.mode.apipool_mcphub_source_deduped", "name", s.Name)
				continue
			}
			kept = append(kept, s)
		}
		if len(kept) == 0 {
			return nil, errors.New(i18n.T("provider.mcphub.no_backends"))
		}
		hubCfg.Servers = kept
	}
	hub, err := mcphub.New(hubCfg, conf.Proxy.ProxyResolver(), nil)
	if err != nil {
		return nil, err
	}
	return hub, nil
}

// BuildBaiduMode 百度千帆搜索（enable_ai_search 控制端点，失败自动回退百度网页搜索）。
func BuildBaiduMode(conf config.Config, pool *provider.KeyPool, baiduWeb *adapter.EngineSearchAdapter, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if conf.APISourceEnabled("baidu") && pool != nil {
		primary := newBaiduSearchFromConf(pool, conf)
		if baiduWeb != nil {
			if conf.Baidu.EnableAISearch {
				log.InfoT("log.search.mode.search_mode_baidu_ai_search_web")
			} else {
				log.InfoT("log.search.mode.search_mode_baidu_qianfan_sk_web")
			}
			return adapter.NewBaiduWithFallback(primary, baiduWeb)
		}
		return primary
	}
	if baiduWeb != nil {
		log.InfoT("log.search.mode.search_mode_baidu_web_search_no")
		return baiduWeb
	}
	if conf.Bing.Enabled && fallback != nil {
		log.ErrorT("log.search.mode.mode_baidu_api_source_unavailable_and")
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
	if !conf.APISourceEnabled("tavily") || pool == nil {
		log.ErrorT("log.search.mode.mode_tavily_api_source_disabled_or")
		if conf.Bing.Enabled && fallback != nil {
			return fallback
		}
		return nil
	}
	return provider.NewTavilySearch(pool, conf.BlackListHost, tavilyOptions(conf)...)
}

// BuildExaMode Exa 单引擎模式。
func BuildExaMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if !conf.APISourceEnabled("exa") || pool == nil {
		log.ErrorT("log.search.mode.mode_exa_api_source_disabled_or")
		if conf.Bing.Enabled && fallback != nil {
			return fallback
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
	return provider.NewExaSearchWithResults(pool, numResults, lookbackDays, conf.BlackListHost, exaOptions(conf)...)
}

// BuildAnysearchMode AnySearch 单引擎模式。
func BuildAnysearchMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if !conf.APISourceEnabled("anysearch") {
		if conf.Bing.Enabled && fallback != nil {
			return fallback
		}
		return nil
	}
	if pool == nil {
		// 零 Key 匿名档（P0-2 来源扩充）：上游支持免鉴权匿名 IP 限流档，
		// 本项目出口实测可用（2026-10-06）；匿名档受 provider 内保守限流钳制
		log.InfoT("log.search.mode.mode_anysearch_no_key_configured_using")
		return provider.NewAnysearchSearch(nil, conf.Anysearch.NumResults, conf.BlackListHost)
	}
	return provider.NewAnysearchSearch(pool, conf.Anysearch.NumResults, conf.BlackListHost)
}

// BuildDoubaoMode 豆包联网搜索单引擎模式。
func BuildDoubaoMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if !conf.APISourceEnabled("doubao") || pool == nil {
		log.ErrorT("log.search.mode.mode_doubao_api_source_disabled_or")
		if conf.Bing.Enabled && fallback != nil {
			return fallback
		}
		return nil
	}
	return newDoubaoFromConf(pool, conf)
}

// BuildYuanbaoMode 腾讯云元宝搜索单引擎模式（API KEY 方式）。
func BuildYuanbaoMode(conf config.Config, pool *provider.KeyPool, fallback *adapter.BingSearchAdapter) core.SearchInf {
	if !conf.APISourceEnabled("yuanbao") || pool == nil {
		log.ErrorT("log.search.mode.mode_yuanbao_api_source_disabled_or")
		if conf.Bing.Enabled && fallback != nil {
			return fallback
		}
		return nil
	}
	return newYuanbaoFromConf(pool, conf)
}

// ProviderPools groups the optional API-key pools by provider. Named fields
// keep call sites readable and stop a new provider from silently shifting
// positional arguments.
type ProviderPools struct {
	Anysearch *provider.KeyPool
	Baidu     *provider.KeyPool
	Tavily    *provider.KeyPool
	Exa       *provider.KeyPool
	Doubao    *provider.KeyPool
	Yuanbao   *provider.KeyPool
	Tinyfish  *provider.KeyPool
	Keenable  *provider.KeyPool
}

// BuildApipoolMode API Key 池轮转模式：每次请求只调用一个供应商，失败自动切换下一个。
// 跨请求时供应商选择由策略决定（round-robin / priority / weighted），Key 均 round-robin 轮转。
// 供应商顺序由 conf.Apipool.Engines 配置控制（默认 anysearch → baidu → tavily → exa）。
// 百度端点由 baidu.enable_ai_search 配置控制（默认 true=智能搜索）。
func BuildApipoolMode(conf config.Config, pools ProviderPools, baiduWeb *adapter.EngineSearchAdapter) core.SearchInf {
	anysearchPool, baiduPool := pools.Anysearch, pools.Baidu
	tavilyPool, exaPool := pools.Tavily, pools.Exa
	doubaoPool, yuanbaoPool := pools.Doubao, pools.Yuanbao
	tinyfishPool, keenablePool := pools.Tinyfish, pools.Keenable
	// 按配置顺序构建供应商（web 兜底始终追加在末尾）
	var providers []apipool.ApipoolProvider
	insertMCPHubAt := -1
	for _, name := range conf.Apipool.GetEngines() {
		if !conf.APISourceEnabled(name) {
			continue
		}
		switch name {
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
		case "yuanbao":
			if yuanbaoPool != nil {
				providers = append(providers, apipool.NewApipoolProvider("yuanbao", newYuanbaoFromConf(yuanbaoPool, conf), yuanbaoPool))
			}
		case "tinyfish":
			// 仅 Key 档：未配置 Key 时 pool 为 nil，直接跳过
			if tinyfishPool != nil {
				providers = append(providers, apipool.NewApipoolProvider("tinyfish", provider.NewTinyFishSearch(tinyfishPool), tinyfishPool))
			}
		case "keenable":
			// keyed 端点按 credits 计费；pool 为 nil 走 public 免 Key 档（按 IP 限流）
			keenable := provider.NewKeenableSearch(keenablePool, conf.Keenable.IsLive(), 0)
			providers = append(providers, apipool.NewApipoolProvider("keenable", keenable, keenablePool))
		case "mcphub":
			// 先占位，等全部供应商装好后再构建：去重需要知道哪些来源已在 apipool 里。
			insertMCPHubAt = len(providers)
		default:
			log.InfoT("log.search.mode.apipool_unknown_provider_skipped", "name", name)
		}
	}
	// 后备补充源：未显式写进 engines 时，把托管 MCP 级联追加在已配置供应商之后、
	// baidu_web 之前。无 Key 可用，能分摊匿名限流、提供额外搜索用量。
	if conf.Apipool.MCPHubFallback && insertMCPHubAt < 0 {
		insertMCPHubAt = len(providers)
	}
	if insertMCPHubAt >= 0 {
		taken := make(map[string]bool, len(providers))
		for _, p := range providers {
			taken[p.Name()] = true
		}
		if hub, err := buildApipoolMCPHub(conf, taken); err == nil {
			p := apipool.NewApipoolProvider("mcphub", hub, nil)
			providers = append(providers, p)
			copy(providers[insertMCPHubAt+1:], providers[insertMCPHubAt:len(providers)-1])
			providers[insertMCPHubAt] = p
		} else {
			log.WarnT("log.search.mode.apipool_mcphub_unavailable", "err", err)
		}
	}
	// 百度网页搜索作为最终兜底（无需 Key，始终在末尾）
	if baiduWeb != nil {
		providers = append(providers, apipool.NewApipoolProvider("baidu_web", baiduWeb, nil))
	}
	if len(providers) == 0 {
		log.ErrorT("log.search.mode.apipool_mode_has_no_available_search")
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
func BuildHybridMode(conf config.Config, pools ProviderPools, ads EngineAdapters, fallback *adapter.BingSearchAdapter) core.SearchInf {
	anysearchPool, baiduPool := pools.Anysearch, pools.Baidu
	tavilyPool, exaPool := pools.Tavily, pools.Exa
	doubaoPool, yuanbaoPool := pools.Doubao, pools.Yuanbao
	tinyfishPool, keenablePool := pools.Tinyfish, pools.Keenable
	var engines []core.SearchInf
	if conf.APISourceEnabled("anysearch") {
		// A nil pool uses the anonymous tier, governed by the same source switch.
		engines = append(engines, provider.NewAnysearchSearch(anysearchPool, conf.Anysearch.NumResults, conf.BlackListHost))
	}
	if conf.APISourceEnabled("baidu") && baiduPool != nil {
		engines = append(engines, newBaiduSearchFromConf(baiduPool, conf))
	}
	if ads.BaiduWeb != nil {
		engines = append(engines, ads.BaiduWeb)
	}
	if ads.So360 != nil {
		engines = append(engines, ads.So360)
	}
	if ads.Sogou != nil {
		engines = append(engines, ads.Sogou)
	}
	if ads.Mojeek != nil {
		engines = append(engines, ads.Mojeek)
	}
	if ads.Wikipedia != nil {
		engines = append(engines, ads.Wikipedia)
	}
	if ads.GNews != nil {
		engines = append(engines, ads.GNews)
	}
	if ads.GitHub != nil {
		engines = append(engines, ads.GitHub)
	}
	if conf.APISourceEnabled("tavily") && tavilyPool != nil {
		engines = append(engines, provider.NewTavilySearch(tavilyPool, conf.BlackListHost, tavilyOptions(conf)...))
	}
	if conf.APISourceEnabled("exa") && exaPool != nil {
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
	if conf.APISourceEnabled("doubao") && doubaoPool != nil {
		engines = append(engines, newDoubaoFromConf(doubaoPool, conf))
	}
	if conf.APISourceEnabled("yuanbao") && yuanbaoPool != nil {
		engines = append(engines, newYuanbaoFromConf(yuanbaoPool, conf))
	}
	if conf.APISourceEnabled("tinyfish") && tinyfishPool != nil {
		engines = append(engines, provider.NewTinyFishSearch(tinyfishPool))
	}
	if conf.APISourceEnabled("keenable") && (keenablePool != nil || conf.Keenable.PublicTierOptedIn()) {
		engines = append(engines, provider.NewKeenableSearch(keenablePool, conf.Keenable.IsLive(), 0))
	}
	if conf.Bing.Enabled && fallback != nil {
		engines = append(engines, fallback)
	}
	if ads.Google != nil {
		engines = append(engines, ads.Google)
	}
	if ads.DDG != nil {
		engines = append(engines, ads.DDG)
	}
	if len(engines) == 0 {
		log.ErrorT("log.search.mode.hybrid_mode_has_no_available_search")
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
		log.InfoT("log.search.mode.baidu_web_search_engine_disabled_baidu")
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
	log.InfoT("log.search.mode.baidu_web_search_engine_enabled_tn")
	return a
}

// InitGoogleEngine 初始化 Google 引擎。
func InitGoogleEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.Google.Enabled {
		log.InfoT("log.search.mode.google_engine_disabled_google_enabled_false")
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
	log.InfoT("log.search.mode.google_engine_enabled_note_may_be")
	return a
}

// InitDuckDuckGoEngine 初始化 DuckDuckGo 引擎。
func InitDuckDuckGoEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.DuckDuckGo.Enabled {
		log.InfoT("log.search.mode.duckduckgo_engine_disabled_duckduckgo_enabled_false")
		return nil
	}
	if conf.Proxy.ProxyResolver() == nil {
		log.InfoT("log.search.mode.duckduckgo_engine_skipped_proxy_required")
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
	log.InfoT("log.search.mode.duckduckgo_engine_enabled_proxy_auto_detected")
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
	log.InfoT("log.search.mode.360_search_engine_enabled_direct_connection")
	return a
}

// InitSogouEngine 初始化搜狗引擎（国内直连可用，零 Key；命中 antispider 时透传拦截错误）。
func InitSogouEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.Sogou.Enabled {
		return nil
	}
	blocked := bing.MergeBlocked(conf.BlackListHost, conf.Sogou.Blocked)
	eng := sogou.NewSogou(sogou.SogouOpts{
		Enabled: true,
		Blocked: blocked,
		PerSec:  conf.Sogou.PerSec,
		PerMin:  conf.Sogou.PerMin,
	})
	a := adapter.NewEngineSearchAdapter("sogou", eng)
	log.InfoT("log.search.mode.sogou_engine_enabled_direct_connection")
	return a
}

// InitMojeekEngine 初始化 Mojeek 引擎（独立英文索引，零 Key；altcha PoW 挑战由引擎内求解）。
func InitMojeekEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.Mojeek.Enabled {
		return nil
	}
	blocked := bing.MergeBlocked(conf.BlackListHost, conf.Mojeek.Blocked)
	eng := mojeek.NewMojeek(mojeek.MojeekOpts{
		Enabled:    true,
		Blocked:    blocked,
		PerSec:     conf.Mojeek.PerSec,
		PerMin:     conf.Mojeek.PerMin,
		SafeSearch: conf.Mojeek.SafeSearch,
	})
	a := adapter.NewEngineSearchAdapter("mojeek", eng)
	log.InfoT("log.search.mode.mojeek_engine_enabled_altcha")
	return a
}

// InitWikipediaEngine 初始化维基百科引擎（MediaWiki API，零 Key；国内出口需代理）。
func InitWikipediaEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.Wikipedia.Enabled {
		return nil
	}
	if conf.Proxy.ProxyResolver() == nil {
		log.InfoT("log.search.mode.wikipedia_engine_skipped_proxy_required")
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
	log.InfoT("log.search.mode.wikipedia_engine_enabled_proxy_auto_detected")
	return a
}

// InitGoogleNewsEngine 初始化 Google News RSS 引擎（零 Key；国内出口需代理）。
func InitGoogleNewsEngine(conf config.Config) *adapter.EngineSearchAdapter {
	if !conf.GoogleNews.Enabled {
		return nil
	}
	if conf.Proxy.ProxyResolver() == nil {
		log.InfoT("log.search.mode.google_news_engine_skipped_proxy_required")
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
	log.InfoT("log.search.mode.google_news_engine_enabled_proxy_auto")
	return a
}

// InitGithubEngine 初始化 GitHub 搜索引擎（REST API，零 Key 匿名档，PAT 提额）。
// bing 非 nil 时 API 失败（限流/403/零结果）自动回退 Bing site:github.com 检索。
// 返回接口类型：具体指针的 nil 赋给 EngineAdapters.GitHub（core.SearchInf）会变成
// typed-nil，`ads.GitHub != nil` 恒真，禁用的引擎会被装进模式。
func InitGithubEngine(conf config.Config, bing *adapter.BingSearchAdapter) core.SearchInf {
	if !conf.GitHub.Enabled {
		log.InfoT("log.search.mode.github_engine_disabled_github_enabled_false")
		return nil
	}
	eng := github.NewGitHub(github.GitHubOpts{
		Enabled:    true,
		Tokens:     conf.GitHub.EffectiveTokenList(),
		NumResults: conf.GitHub.NumResults,
		PerMin:     conf.GitHub.PerMin,
	})
	primary := adapter.NewEngineSearchAdapter("github", eng)
	var fb core.SearchInf
	if bing != nil {
		fb = bing
	}
	if conf.GitHub.HasToken() {
		log.InfoT("log.search.mode.github_engine_enabled_rest_api_pat")
	} else {
		log.InfoT("log.search.mode.github_engine_enabled_rest_api_anonymous")
	}
	return adapter.NewGitHubWithFallback(primary, fb)
}

// InitBingEngine 根据配置初始化 Bing 引擎适配器（各模式通用兜底）。
func InitBingEngine(conf config.Config) *adapter.BingSearchAdapter {
	if !conf.Bing.Enabled {
		log.InfoT("log.search.mode.bing_engine_disabled_bing_enabled_false")
		return nil
	}

	bingOpts := bing.BingOpts{
		Enabled: true,
		Blocked: bing.MergeBlocked(conf.BlackListHost, conf.Bing.Blocked),
		PerSec:  conf.GetRateLimitPerSec(),
		PerMin:  conf.GetRateLimitPerMin(),
	}

	fallback := adapter.NewBingSearchAdapter(bingOpts)
	log.InfoT("log.search.mode.bing_engine_enabled_engines", "engines", fallback.Engines())
	return fallback
}

// InitAcademicEngine 根据配置初始化学术搜索引擎。
func InitAcademicEngine(conf config.Config) core.AcademicSearcher {
	acad := conf.Academic
	if !acad.Enabled {
		log.InfoT("log.search.mode.academic_engines_not_enabled_academic_enabled")
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
		OpenAlex:        antirobot.OpenAlexOpts{Enabled: !acad.DisableOpenAlex, APIKey: acad.OpenAlexAPIKey},
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
		log.InfoT("log.search.mode.no_available_academic_engines")
		return nil
	}

	// 学术评分增强（默认开启，配置独立于 smartsearch，属 academicsearch 工具）
	a.SetEnhance(conf.Academic.Enhance, conf.Academic.Threshold)
	if conf.Academic.Enhance {
		log.InfoT("log.search.mode.academic_score_enhancement_enabled_threshold", "threshold", conf.Academic.Threshold)
	}

	log.InfoT("log.search.mode.academic_engines_enabled", "engines", a.Engines())
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

func newYuanbaoFromConf(pool *provider.KeyPool, conf config.Config) *provider.YuanbaoSearchImpl {
	return provider.NewYuanbaoSearch(pool, provider.YuanbaoOptions{
		NumResults:     conf.Yuanbao.NumResults,
		Mode:           conf.Yuanbao.Mode,
		Site:           conf.Yuanbao.Site,
		Industry:       conf.Yuanbao.Industry,
		Freshness:      conf.Yuanbao.Freshness,
		ExcludeDomains: conf.BlackListHost,
		MonthlyLimit:   conf.Yuanbao.GetMonthlyLimit(),
		StatePath:      conf.GetYuanbaoStatePath(),
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
		log.InfoT("log.search.mode.wigolo_score_enhancement_enabled_threshold", "relevanceThreshold", sc.RelevanceThreshold)
	}
	// MMR 多样性重排（默认启用）
	hs.SetMMR(sc.MMR)
	if enhance && sc.MMR.Enabled {
		log.InfoT("log.search.mode.mmr_diversity_re_ranking_enabled_lambda", "lambda", sc.MMR.Lambda)
	}
	// off-topic 整桶守卫（P1-7 回查处置：配置省略 = shadow 只记录不丢弃，enforce 须显式开启）
	guardMode := sc.OffTopicGuardMode()
	hs.SetOffTopicGuard(guardMode)
	log.InfoT("log.search.mode.off_topic_whole_bucket_guard_mode", "guardMode", guardMode)
	// per-engine 软超时：慢引擎按 timeout 进失败清单，其余引擎结果照常返回
	if d := sc.EngineTimeout(); d > 0 {
		hs.SetEngineTimeout(d)
	}
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
