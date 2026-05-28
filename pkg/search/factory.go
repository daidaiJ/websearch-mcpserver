package search

import (
	"fmt"
	"websearch/pkg/antirobot"
	"websearch/pkg/bing"
	"websearch/pkg/config"
	"websearch/pkg/log"
)

// SearchGroup 搜索引擎组，包含主引擎、兜底引擎和学术引擎。
type SearchGroup struct {
	Primary  SearchInf          // 主搜索引擎
	Fallback *BingSearchAdapter // Bing 兜底引擎（可为 nil）
	Academic AcademicSearcher   // 学术搜索引擎（可为 nil）
}

// NewFromConfig 根据配置初始化搜索引擎组。
func NewFromConfig(conf config.Config) (*SearchGroup, error) {
	g := &SearchGroup{}

	// ── 初始化 Bing 引擎（兜底） ──
	initBingEngine(conf, g)

	// ── 按模式选择主引擎 ──
	switch conf.GetMode() {
	case config.ModeEngine:
		if g.Fallback == nil {
			return nil, fmt.Errorf("engine 模式需要 bing 引擎，请检查 bing 配置")
		}
		g.Primary = g.Fallback
		log.Info("搜索模式: engine（纯引擎模式，无需 API Key）")

	case config.ModeTavily:
		if conf.Tavily.APIKey == "" {
			log.Error("mode=tavily 但未配置 tavily.api_key，回退到 engine 模式")
			if g.Fallback != nil {
				g.Primary = g.Fallback
			} else {
				return nil, fmt.Errorf("无可用搜索引擎")
			}
		} else {
			g.Primary = NewTavilySearch(conf.Tavily.APIKey, conf.BlackListHost)
		}

	case config.ModeHybrid:
		var engines []SearchInf
		if conf.Baidu.APIKey != "" {
			engines = append(engines, NewBaiduSeach(conf.Baidu.APIKey, conf.BlackListHost))
		}
		if conf.Tavily.APIKey != "" {
			engines = append(engines, NewTavilySearch(conf.Tavily.APIKey, conf.BlackListHost))
		}
		// Bing 作为原生引擎参与混合搜索
		if g.Fallback != nil {
			engines = append(engines, g.Fallback)
		}
		if len(engines) == 0 {
			return nil, fmt.Errorf("无可用搜索引擎，请检查配置")
		}
		g.Primary = NewHybridSearch(engines...)

	default: // baidu
		if conf.Baidu.APIKey == "" {
			log.Error("mode=baidu 但未配置 baidu.api_key，回退到 engine 模式")
			if g.Fallback != nil {
				g.Primary = g.Fallback
			} else {
				return nil, fmt.Errorf("无可用搜索引擎")
			}
		} else {
			g.Primary = NewBaiduSeach(conf.Baidu.APIKey, conf.BlackListHost)
		}
	}

	log.Infof("搜索模式: %s", conf.GetMode())

	// ── 学术搜索引擎（独立于主引擎） ──
	initAcademicEngine(conf, g)

	return g, nil
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
	}
	if conf.Bing.PerSec > 0 {
		bingOpts.PerSec = conf.Bing.PerSec
	}
	if conf.Bing.PerMin > 0 {
		bingOpts.PerMin = conf.Bing.PerMin
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

	proxyEndpoint := ""
	if conf.Proxy.Enabled {
		proxyEndpoint = conf.Proxy.GetProxyEndpoint()
	}

	acadConf := AcademicConfig{
		Network:         network,
		Arxiv:           antirobot.ArxivOpts{Enabled: !acad.DisableArxiv},
		Crossref:        antirobot.CrossrefOpts{Enabled: !acad.DisableCrossref},
		OpenAlex:        antirobot.OpenAlexOpts{Enabled: !acad.DisableOpenAlex},
		SemanticScholar: antirobot.SemanticScholarOpts{Enabled: !acad.DisableSemanticScholar},
		PubMed:          antirobot.PubMedOpts{Enabled: !acad.DisablePubMed},
		GoogleScholar:   antirobot.GoogleScholarOpts{Enabled: !acad.DisableGoogleScholar},
		Proxy:           proxyEndpoint,
	}

	adapter := NewAcademicAdapter(acadConf)
	if adapter == nil {
		log.Info("无可用学术引擎")
		return
	}

	g.Academic = adapter
	engines := adapter.Engines()
	log.Infof("学术引擎已启用: %v", engines)
}
