package mode

import (
	"reflect"

	"websearch/pkg/config"
	"websearch/pkg/search/core"
	"websearch/pkg/search/provider"
)

// NewDiagnosticSource builds a standalone adapter for the selected source.
func NewDiagnosticSource(conf config.Config, id string, pool *provider.KeyPool) core.ContextSearcher {
	var engine core.SearchInf
	switch id {
	case "baidu":
		engine = BuildBaiduMode(conf, pool, nil, nil)
	case "tavily":
		engine = BuildTavilyMode(conf, pool, nil)
	case "exa":
		engine = BuildExaMode(conf, pool, nil)
	case "anysearch":
		engine = BuildAnysearchMode(conf, pool, nil)
	case "doubao":
		engine = BuildDoubaoMode(conf, pool, nil)
	case "yuanbao":
		engine = BuildYuanbaoMode(conf, pool, nil)
	case "tinyfish":
		if pool != nil {
			engine = provider.NewTinyFishSearch(pool)
		}
	case "keenable":
		engine = provider.NewKeenableSearch(pool, conf.Keenable.IsLive(), 0)
	case "baidu_web":
		engine = InitBaiduWebEngine(conf)
	case "bing":
		engine = InitBingEngine(conf)
	case "github":
		engine = InitGithubEngine(conf, nil)
	case "so360":
		engine = InitSo360Engine(conf)
	case "sogou":
		engine = InitSogouEngine(conf)
	case "mojeek":
		engine = InitMojeekEngine(conf)
	case "wikipedia":
		engine = InitWikipediaEngine(conf)
	case "googlenews":
		engine = InitGoogleNewsEngine(conf)
	case "google":
		engine = InitGoogleEngine(conf)
	case "duckduckgo":
		engine = InitDuckDuckGoEngine(conf)
	}
	if engine == nil || reflect.ValueOf(engine).IsNil() {
		return nil
	}
	contextual, _ := engine.(core.ContextSearcher)
	return contextual
}
