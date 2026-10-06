// Package search 搜索编排组合根：SearchGroup 装配与类型契约别名。
// 各搜索模式的构建逻辑见子包 mode；多引擎并发编排见子包 hybrid。
package search

import (
	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/search/adapter"
	"websearch/pkg/search/core"
	"websearch/pkg/search/mode"
	"websearch/pkg/search/provider"
)

// SearchGroup 搜索引擎组，包含主引擎、兜底引擎和学术引擎。
type SearchGroup struct {
	Primary  core.SearchInf            // 主搜索引擎
	Fallback *adapter.BingSearchAdapter // Bing 兜底引擎（可为 nil）
	Academic core.AcademicSearcher     // 学术搜索引擎（可为 nil）
	conf     config.Config             // 保存配置，用于代理变更时重建引擎
}

// NewFromConfig 根据配置初始化搜索引擎组（组合根：只做装配，模式构建在子包 mode）。
func NewFromConfig(conf config.Config) (*SearchGroup, error) {
	g := &SearchGroup{conf: conf}

	core.ShowMeta = conf.SmartSearch.ShowMeta

	// ── 初始化 Bing 引擎（兜底） ──
	g.Fallback = mode.InitBingEngine(conf)

	// ── 初始化百度网页搜索引擎（无需 API Key，SK 失败时回退） ──
	baiduWebAdapter := mode.InitBaiduWebEngine(conf)

	// ── 初始化 360 搜索引擎（无需 API Key，国内直连可用） ──
	so360Adapter := mode.InitSo360Engine(conf)

	// ── 初始化 Wikipedia 引擎（无需 API Key，需代理） ──
	wikiAdapter := mode.InitWikipediaEngine(conf)

	// ── 初始化 Google News 引擎（无需 API Key，需代理） ──
	gnewsAdapter := mode.InitGoogleNewsEngine(conf)

	// ── 初始化 Google 引擎（需代理，由 resolver 动态解析） ──
	googleAdapter := mode.InitGoogleEngine(conf)

	// ── 初始化 DuckDuckGo 引擎（需代理，由 resolver 动态解析） ──
	ddgAdapter := mode.InitDuckDuckGoEngine(conf)

	// engine/hybrid 模式的通用引擎适配器集合
	ads := mode.EngineAdapters{
		BaiduWeb:  baiduWebAdapter,
		So360:     so360Adapter,
		Wikipedia: wikiAdapter,
		GNews:     gnewsAdapter,
		Google:    googleAdapter,
		DDG:       ddgAdapter,
	}

	// ── 构建 provider.KeyPool ──
	baiduPool := newKeyPoolFromList(conf.Baidu.EffectiveSKList(), "baidu")
	tavilyPool := newKeyPoolFromList(conf.Tavily.EffectiveSKList(), "tavily")
	exaPool := newKeyPoolFromList(conf.Exa.EffectiveSKList(), "exa")
	anysearchPool := newKeyPoolFromList(conf.Anysearch.EffectiveSKList(), "anysearch")
	doubaoPool := newKeyPoolFromList(conf.Doubao.EffectiveSKList(), "doubao")

	// ── 按模式选择主引擎 ──
	switch conf.GetMode() {
	case config.ModeEngine:
		g.Primary = mode.BuildEngineMode(conf, g.Fallback, ads)
		log.Infof("搜索模式: engine（无需 API Key）")

	case config.ModeTavily:
		g.Primary = mode.BuildTavilyMode(conf, tavilyPool, g.Fallback)

	case config.ModeExa:
		g.Primary = mode.BuildExaMode(conf, exaPool, g.Fallback)

	case config.ModeAnysearch:
		g.Primary = mode.BuildAnysearchMode(conf, anysearchPool, g.Fallback)

	case config.ModeDoubao:
		g.Primary = mode.BuildDoubaoMode(conf, doubaoPool, g.Fallback)

	case config.ModeApipool:
		g.Primary = mode.BuildApipoolMode(conf, anysearchPool, baiduPool, tavilyPool, exaPool, doubaoPool, baiduWebAdapter)
		log.Infof("搜索模式: apipool（API Key 池轮转）")

	case config.ModeHybrid:
		g.Primary = mode.BuildHybridMode(conf, anysearchPool, baiduPool, tavilyPool, exaPool, doubaoPool, ads, g.Fallback)

	default: // baidu → 百度千帆 web_search
		g.Primary = mode.BuildBaiduMode(conf, baiduPool, baiduWebAdapter, g.Fallback)
	}

	log.Infof("搜索模式: %s", conf.GetMode())

	// ── 学术搜索引擎（独立于主引擎） ──
	g.Academic = mode.InitAcademicEngine(conf)

	return g, nil
}

// newKeyPoolFromList 从 key 列表创建 provider.KeyPool，列表为空时返回 nil。
func newKeyPoolFromList(keys []string, name string) *provider.KeyPool {
	if len(keys) == 0 {
		return nil
	}
	pool, err := provider.NewKeyPool(keys)
	if err != nil {
		log.Errf("创建 %s provider.KeyPool 失败: %v", name, err)
		return nil
	}
	if pool.Len() > 1 {
		log.Infof("%s provider.KeyPool: %d 个 Key 轮询", name, pool.Len())
	}
	return pool
}
