package search

import (
	"fmt"
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
// 包含 Bing 引擎初始化（兜底）和按模式选择主引擎。
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
		if conf.TavilySk == "" {
			log.Error("mode=tavily 但未配置 tavily_sk，回退到 engine 模式")
			if g.Fallback != nil {
				g.Primary = g.Fallback
			} else {
				return nil, fmt.Errorf("无可用搜索引擎")
			}
		} else {
			g.Primary = NewTavilySearch(conf.TavilySk)
		}

	case config.ModeHybrid:
		var engines []SearchInf
		if conf.BaiduSK != "" {
			engines = append(engines, NewBaiduSeach(conf.BaiduSK, conf.BlackListHost))
		}
		if conf.TavilySk != "" {
			engines = append(engines, NewTavilySearch(conf.TavilySk))
		}
		if len(engines) == 0 {
			log.Error("mode=hybrid 但未配置任何 API Key，回退到 engine 模式")
			if g.Fallback != nil {
				g.Primary = g.Fallback
			} else {
				return nil, fmt.Errorf("无可用搜索引擎")
			}
		} else {
			g.Primary = NewHybridSearch(engines...)
		}

	default: // baidu
		if conf.BaiduSK == "" {
			log.Error("mode=baidu 但未配置 baidu_sk，回退到 engine 模式")
			if g.Fallback != nil {
				g.Primary = g.Fallback
			} else {
				return nil, fmt.Errorf("无可用搜索引擎")
			}
		} else {
			g.Primary = NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
		}
	}

	log.Infof("搜索模式: %s", conf.GetMode())

	// ── 学术搜索引擎：优先从主引擎获取，否则从 Bing 引擎获取 ──
	if as, ok := g.Primary.(AcademicSearcher); ok {
		g.Academic = as
		log.Info("学术搜索引擎已就绪（主引擎）")
	} else if g.Fallback != nil {
		g.Academic = g.Fallback
		log.Info("学术搜索引擎已就绪（Bing 引擎）")
	}

	return g, nil
}

// initBingEngine 根据配置初始化 Bing 引擎适配器。
// 默认所有引擎开启，通过 disable_* 字段关闭特定引擎。
func initBingEngine(conf config.Config, g *SearchGroup) {
	bc := conf.Bing
	if !bc.Enabled {
		log.Info("Bing 引擎已禁用（bing.enabled=false）")
		return
	}

	regularOpts := bing.DefaultOptions()
	regularOpts.Bing.Blocked = bc.Blocked
	if bc.PerSec > 0 {
		regularOpts.Bing.PerSec = bc.PerSec
	}
	if bc.PerMin > 0 {
		regularOpts.Bing.PerMin = bc.PerMin
	}

	academicOpts := regularOpts
	if bc.Academic {
		academicOpts.Academic = true
		academicOpts.Arxiv.Enabled = !bc.DisableArxiv
		academicOpts.Crossref.Enabled = !bc.DisableCrossref
		academicOpts.OpenAlex.Enabled = !bc.DisableOpenAlex
		academicOpts.SemanticScholar.Enabled = !bc.DisableSemanticScholar

		if bc.IsInternational() {
			academicOpts.Network = bing.RegionInternational
		} else {
			academicOpts.Network = bing.RegionChina
			if academicOpts.Arxiv.Enabled {
				log.Info("国内网络环境，arXiv 引擎将由引擎调度器自动跳过")
			}
			if academicOpts.SemanticScholar.Enabled {
				log.Info("国内网络环境，Semantic Scholar 引擎将由引擎调度器自动跳过")
			}
		}
	}

	g.Fallback = NewBingSearchAdapter(regularOpts, academicOpts)

	engines := g.Fallback.Engines()
	log.Infof("Bing 引擎已启用，通用引擎: %v", engines)
	if bc.Academic {
		acadEngines := g.Fallback.AcademicEngines()
		log.Infof("学术引擎: %v", acadEngines)
	}
}
