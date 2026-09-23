package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"websearch/pkg/config"
)

// newDashboardMux 注册启用了控制台的 admin 路由，用于访问边界测试。
func newDashboardMux(conf config.Config) *http.ServeMux {
	s := New()
	mux := http.NewServeMux()
	s.registerAdminHandlers(mux, conf, nil)
	return mux
}

func dashboardConf(networks []string) config.Config {
	conf := config.Config{}
	conf.Dashboard.Enabled = true
	conf.Dashboard.AllowedNetworks = networks
	return conf
}

func TestDashboardAccessBoundary(t *testing.T) {
	mux := newDashboardMux(dashboardConf([]string{"192.168.1.0/24"}))

	cases := []struct {
		name, remote, host string
		want               int
	}{
		{"本机直连", "127.0.0.1:1111", "127.0.0.1:8338", http.StatusOK},
		{"IPv6 本机", "[::1]:1111", "localhost:8338", http.StatusOK},
		{"远程默认拒绝", "10.1.2.3:1111", "10.1.2.3:8338", http.StatusForbidden},
		// Host 头可被任意伪造，不再参与判定（回归：PR 里的放宽已移除）
		{"远程伪造 Host localhost", "10.1.2.3:1111", "localhost:8338", http.StatusForbidden},
		{"远程伪造 Host 127.0.0.1", "10.1.2.3:1111", "127.0.0.1:8338", http.StatusForbidden},
		{"白名单网段放行", "192.168.1.66:1111", "192.168.1.66:8338", http.StatusOK},
		{"白名单外拒绝", "192.168.2.66:1111", "192.168.2.66:8338", http.StatusForbidden},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/__admin/api/settings", nil)
		r.RemoteAddr = tc.remote
		r.Host = tc.host
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestDashboardInvalidNetworksFailClosed(t *testing.T) {
	mux := newDashboardMux(dashboardConf([]string{"not-a-cidr"}))
	for _, remote := range []string{"127.0.0.1:1", "192.168.1.66:1"} {
		r := httptest.NewRequest(http.MethodGet, "/__admin/api/settings", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if remote == "127.0.0.1:1" && w.Code != http.StatusOK {
			t.Errorf("非法网段回退后本机应可访问, got %d", w.Code)
		}
		if remote != "127.0.0.1:1" && w.Code != http.StatusForbidden {
			t.Errorf("非法网段必须 fail-closed 拒绝远程, got %d", w.Code)
		}
	}
}

func TestAdminEndpointsStayLoopbackOnly(t *testing.T) {
	// allowed_networks 只放宽控制台读取；进程管理端点仍仅限本机
	mux := newDashboardMux(dashboardConf([]string{"192.168.1.0/24"}))
	r := httptest.NewRequest(http.MethodGet, "/__admin/status", nil)
	r.RemoteAddr = "192.168.1.66:1"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("__admin/status 对白名单网段仍应拒绝, got %d", w.Code)
	}
}
