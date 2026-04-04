package searxng

import (
	"fmt"
	"net/http"
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
	case config.ModeTavily:
		if conf.TavilySk == "" {
			log.Error("mode=tavily 但未配置 tavily_sk，回退到 baidu")
			defaultInf = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
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
			log.Error("mode=hybrid 但未配置任何搜索引擎 sk")
			panic("hybrid 模式需要至少配置 baidu_sk 或 tavily_sk")
		}
		defaultInf = search.NewHybridSearch(engines...)
	default: // baidu
		defaultInf = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
	}
}

func RegisterRouter(mux *http.ServeMux) {
	mux.HandleFunc("GET /searxng/search", handlerSearch)
}
