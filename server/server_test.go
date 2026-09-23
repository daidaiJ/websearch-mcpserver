package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"websearch/pkg/config"
)

// newAdminMux 注册 admin 路由用于测试观测。
func newAdminMux(initRef int32) (*Server, *http.ServeMux) {
	s := New()
	s.SetRefCount(initRef)
	mux := http.NewServeMux()
	s.registerAdminHandlers(mux, *config.Default(), nil)
	return s, mux
}

// doReq 发起请求，remoteAddr 为空则使用本地地址。
func doReq(mux *http.ServeMux, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if remoteAddr != "" {
		r.RemoteAddr = remoteAddr
	} else {
		r.RemoteAddr = "127.0.0.1:12345"
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestAdminHealth(t *testing.T) {
	_, mux := newAdminMux(3)
	// health 应远程可访问
	w := doReq(mux, http.MethodGet, "/__admin/health", "", "8.8.8.8:5555")
	if w.Code != http.StatusOK {
		t.Fatalf("health 远程应可访问, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	t.Logf("health resp: %s", w.Body.String())
	if resp["message"] != "running" {
		t.Errorf(`health message 应为 "running", got %v`, resp["message"])
	}
	if resp["ref_count"] != float64(3) {
		t.Errorf("health ref_count 应为 3, got %v", resp["ref_count"])
	}
}

func TestAdminLocalOnly(t *testing.T) {
	_, mux := newAdminMux(1)
	// status/refcount/shutdown 远程访问应被拒绝
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/__admin/status", ""},
		{http.MethodPost, "/__admin/refcount", `{"delta":1}`},
		{http.MethodPost, "/__admin/shutdown", ""},
	} {
		w := doReq(mux, tc.method, tc.path, tc.body, "8.8.8.8:5555")
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s 远程应返回 403, got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestAdminStatus(t *testing.T) {
	_, mux := newAdminMux(2)
	w := doReq(mux, http.MethodGet, "/__admin/status", "", "")
	t.Logf("status resp: %s", w.Body.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status 本地应可访问, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ref_count"] != float64(2) {
		t.Errorf("status ref_count 应为 2, got %v", resp["ref_count"])
	}
	// 非 GET 方法应被拒绝
	w = doReq(mux, http.MethodPost, "/__admin/status", "", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status POST 应返回 405, got %d", w.Code)
	}
}

func TestAdminRefCount(t *testing.T) {
	s, mux := newAdminMux(1)
	// +2 → 3
	w := doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":2}`, "")
	t.Logf("refcount +2 resp: %s", w.Body.String())
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ref_count"] != float64(3) {
		t.Errorf("refcount 应为 3, got %v", resp["ref_count"])
	}
	if s.RefCount() != 3 {
		t.Errorf("server refCount 应为 3, got %d", s.RefCount())
	}
	// 归零触发关闭消息
	w = doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":-3}`, "")
	t.Logf("refcount -3 resp: %s", w.Body.String())
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ref_count"] != float64(0) {
		t.Errorf("refcount 应为 0, got %v", resp["ref_count"])
	}
	if resp["message"] == "" || resp["message"] == nil {
		t.Errorf("归零时应返回 shutdown 消息, got %v", resp["message"])
	}
}
