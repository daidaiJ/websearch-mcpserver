package searxng

import (
	"encoding/json"
	"net/http"
	"websearch/pkg/log"
)

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

func responseError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	resp := map[string]interface{}{
		"code": code,
		"msg":  msg,
	}
	json.NewEncoder(w).Encode(resp)
}
