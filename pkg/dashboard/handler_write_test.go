package dashboard

import (
	"net/http"
	"strings"
	"net/http/httptest"
	"testing"

	"websearch/pkg/config"
)

// newWriteTestHandler 构造带管理员口令的 Handler；guard 为直通，
// 专测 handler 内部的写闸门（loopback + 口令）。
func newWriteTestHandler(t *testing.T, conf config.Config) (*Handler, *http.ServeMux) {
	t.Helper()
	base, _ := newTestHandler(t)
	handler := New(conf, base.store, nil, nil)
	mux := http.NewServeMux()
	handler.Register(mux, func(fn http.HandlerFunc) http.HandlerFunc { return fn })
	return handler, mux
}

func doWrite(t *testing.T, mux *http.ServeMux, method, path, body, remoteAddr, pass string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if remoteAddr != "" {
		r.RemoteAddr = remoteAddr
	} else {
		r.RemoteAddr = "127.0.0.1:12345"
	}
	if pass != "" {
		r.Header.Set("X-Admin-Password", pass)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestWriteGateMatrix(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	_, mux := newWriteTestHandler(t, conf)

	// 读端点不受口令限制，远程（allowed_networks 场景）也可读
	if code := doWrite(t, mux, http.MethodGet, "/__admin/api/settings", "", "192.168.1.9:5", "").Code; code != http.StatusOK {
		t.Fatalf("远程读取设置应放行, got %d", code)
	}

	cases := []struct {
		name, path, body, remote, pass string
		want                           int
	}{
		{"本机+正确口令-重启", "/__admin/api/restart", `{"confirm":true}`, "127.0.0.1:1", "pw", http.StatusAccepted},
		{"本机+无口令-重启", "/__admin/api/restart", `{"confirm":true}`, "127.0.0.1:1", "", http.StatusForbidden},
		{"本机+错误口令-重启", "/__admin/api/restart", `{"confirm":true}`, "127.0.0.1:1", "bad", http.StatusForbidden},
		{"远程+正确口令-重启", "/__admin/api/restart", `{"confirm":true}`, "192.168.1.9:5", "pw", http.StatusForbidden},
		{"本机+口令-设置预览", "/__admin/api/settings", `{"changes":{"mode":"engine"},"confirm":false}`, "127.0.0.1:1", "pw", http.StatusOK},
		{"本机+无口令-设置", "/__admin/api/settings", `{"changes":{"mode":"engine"},"confirm":false}`, "127.0.0.1:1", "", http.StatusForbidden},
		{"本机+无口令-密钥", "/__admin/api/secrets", `{"name":"TAVILY_SK","value":"x","confirm":true}`, "127.0.0.1:1", "", http.StatusForbidden},
		{"远程+口令-密钥", "/__admin/api/secrets", `{"name":"TAVILY_SK","value":"x","confirm":true}`, "192.168.1.9:5", "pw", http.StatusForbidden},
		{"本机+无口令-清缓存", "/__admin/api/cache/clear", `{"confirm":true}`, "127.0.0.1:1", "", http.StatusForbidden},
		{"本机+无口令-额度重置", "/__admin/api/quotas/reset", `{"provider":"exa"}`, "127.0.0.1:1", "", http.StatusForbidden},
	}
	for _, tc := range cases {
		if code := doWrite(t, mux, http.MethodPost, tc.path, tc.body, tc.remote, tc.pass).Code; code != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, code, tc.want)
		}
	}
}

func TestWriteGateDisabledWithoutPassword(t *testing.T) {
	_, mux := newWriteTestHandler(t, config.Config{})
	// 未配置口令：即便来自本机，写端点也整体禁用
	if code := doWrite(t, mux, http.MethodPost, "/__admin/api/restart", `{"confirm":true}`, "127.0.0.1:1", "").Code; code != http.StatusForbidden {
		t.Fatalf("未配置口令时写操作应禁用, got %d", code)
	}
}

// TestWriteGateUsername 验证可选管理员用户名：配置后 X-Admin-User 必须匹配
// （常量时间比较），未配置 = 完全不做用户名检查（与口令-only 行为一致）。
func TestWriteGateUsername(t *testing.T) {
	do := func(t *testing.T, conf config.Config, user, pass string) int {
		t.Helper()
		_, mux := newWriteTestHandler(t, conf)
		r := httptest.NewRequest(http.MethodPost, "/__admin/api/restart", strings.NewReader(`{"confirm":true}`))
		r.RemoteAddr = "127.0.0.1:1"
		if user != "" {
			r.Header.Set("X-Admin-User", user)
		}
		if pass != "" {
			r.Header.Set("X-Admin-Password", pass)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec.Code
	}

	// 未配置用户名：带不带 X-Admin-User 都只看口令
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	if code := do(t, conf, "", "pw"); code != http.StatusAccepted {
		t.Fatalf("未配置用户名时应只验证口令, got %d", code)
	}
	if code := do(t, conf, "someone", "pw"); code != http.StatusAccepted {
		t.Fatalf("未配置用户名时多余的头应被忽略, got %d", code)
	}

	// 配置用户名：必须匹配
	conf.Dashboard.AdminUsername = "admin"
	if code := do(t, conf, "admin", "pw"); code != http.StatusAccepted {
		t.Fatalf("用户名+口令正确应放行, got %d", code)
	}
	if code := do(t, conf, "", "pw"); code != http.StatusForbidden {
		t.Fatalf("缺用户名应拒绝, got %d", code)
	}
	if code := do(t, conf, "Admin", "pw"); code != http.StatusForbidden {
		t.Fatalf("用户名大小写敏感应拒绝, got %d", code)
	}
	if code := do(t, conf, "admin", "bad"); code != http.StatusForbidden {
		t.Fatalf("用户名对但口令错应拒绝, got %d", code)
	}
}

func TestIsLoopbackRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[::1]:4444"
	if !IsLoopbackRemote(r) {
		t.Fatal("::1 应视为 loopback")
	}
	r.RemoteAddr = "192.168.1.5:9"
	if IsLoopbackRemote(r) {
		t.Fatal("局域网地址不应视为 loopback")
	}
}
