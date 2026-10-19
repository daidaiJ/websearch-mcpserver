package searxng

import (
	"encoding/json"
	"fmt"
	"net/http"

	mcpserver "websearch/mcp"
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
	if query == "" {
		http.Error(w, "missing query parameter q", http.StatusBadRequest)
		return
	}
	if defaultInf == nil {
		http.Error(w, "no search engine available", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	var ret []search.SearchResult
	var err error
	if cs, ok := defaultInf.(search.ContextSearcher); ok {
		ret, err = cs.SearchRawContext(ctx, query)
	} else {
		ret, err = defaultInf.SearchRaw(query)
	}
	if err != nil {
		responseError(w, 5001, fmt.Sprintf("%s", err.Error()))
		return
	}
	log.Infof("search:%s success\n", query)

	responseJSON(w, query, slice2Any(ret))
}

func Init(group *search.SearchGroup) {
	if group.Primary != nil {
		defaultInf = group.Primary
	} else {
		log.Error("无可用搜索引擎")
	}
}

func RegisterRouter(mux *http.ServeMux, conf config.Config) {
	mux.Handle("GET /searxng/search", mcpserver.AuthMiddleware(conf, http.HandlerFunc(handlerSearch)))
}

// responseJSON 输出 JSON 响应。
func responseJSON(w http.ResponseWriter, query string, data []any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]interface{}{
		"query":             query,
		"results":           data,
		"number_of_results": len(data),
	}
	s, _ := json.Marshal(resp)
	log.Debugf("raw msg : %s", s)
	json.NewEncoder(w).Encode(resp)
}

// responseError 输出错误响应。
func responseError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	resp := map[string]interface{}{
		"code": code,
		"msg":  msg,
	}
	json.NewEncoder(w).Encode(resp)
}
