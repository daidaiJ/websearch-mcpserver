package searxng

import (
	"fmt"
	"net/http"
	"strings"
	"websearch/pkg/bing"
	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/search"
)

var defaultInf search.SearchInf

func slice2Any[T any](s []T) []any {
	ret := make([]any, 0, len(s))
	for _, val := range s {
		ret = append(ret, val)
	}
	return ret
}

func handlerSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	ret, err := defaultInf.SearchRaw(query)
	if err != nil {
		responseError(w, 5001, fmt.Sprintf("%s", err.Error()))
		return
	}
	log.Infof("search:%s success\n", query)

	responseJSON(w, query, slice2Any(ret))
}

func Init(conf config.Config) {
	switch conf.GetMode() {
	case config.ModeEngine:
		// 纯引擎模式
		if conf.Bing.Enabled {
			defaultInf = buildBingAdapter(conf)
		} else {
			log.Error("engine 模式但 bing 引擎未启用")
		}

	case config.ModeTavily:
		if conf.TavilySk == "" {
			log.Error("mode=tavily 但未配置 tavily_sk，回退到 engine 模式")
			if conf.Bing.Enabled {
				defaultInf = buildBingAdapter(conf)
			} else {
				log.Error("无可用搜索引擎")
			}
		} else {
			defaultInf = search.NewTavilySearch(conf.TavilySk)
		}

	case config.ModeHybrid:
		var engines []search.SearchInf
		if conf.BaiduSK != "" {
			engines = append(engines, search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost))
		}
		if conf.TavilySk != "" {
			engines = append(engines, search.NewTavilySearch(conf.TavilySk))
		}
		if len(engines) == 0 {
			log.Error("mode=hybrid 但未配置任何 API Key，回退到 engine 模式")
			if conf.Bing.Enabled {
				defaultInf = buildBingAdapter(conf)
			} else {
				log.Error("无可用搜索引擎")
			}
		} else {
			defaultInf = search.NewHybridSearch(engines...)
		}

	default: // baidu
		if conf.BaiduSK == "" {
			log.Error("mode=baidu 但未配置 baidu_sk，回退到 engine 模式")
			if conf.Bing.Enabled {
				defaultInf = buildBingAdapter(conf)
			} else {
				log.Error("无可用搜索引擎")
			}
		} else {
			defaultInf = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
		}
	}
}

// buildBingAdapter 根据配置构建 Bing 引擎适配器。
func buildBingAdapter(conf config.Config) *search.BingSearchAdapter {
	bc := conf.Bing

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
		}
	}

	adapter := search.NewBingSearchAdapter(regularOpts, academicOpts)
	log.Infof("SearXNG 后端使用 Bing 引擎: %v", strings.Join(adapter.Engines(), ", "))
	return adapter
}

func RegisterRouter(mux *http.ServeMux) {
	mux.HandleFunc("GET /searxng/search", handlerSearch)
}
