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
	defaultInf = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
}

func RegisterRouter(mux *http.ServeMux) {
	mux.HandleFunc("GET /searxng/search", handlerSearch)
}
