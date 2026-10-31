package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDefaultBaseTransport_HeaderTimeoutOutlastsCallers 验证响应头兜底超时不先于调用方预算打断请求。
// 调用方自带预算：mcphub 抓取默认 45s、mcphub 搜索默认 20s、pkg/client 默认 30s。
func TestDefaultBaseTransport_HeaderTimeoutOutlastsCallers(t *testing.T) {
	tr := defaultBaseTransport()
	if tr.ResponseHeaderTimeout != defaultResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	}
	const callerBudget = 45 * time.Second // mcp_hub.fetch_timeout_sec 默认值
	if tr.ResponseHeaderTimeout <= callerBudget {
		t.Errorf("ResponseHeaderTimeout = %v，必须大于调用方预算 %v，否则会先于调用方超时", tr.ResponseHeaderTimeout, callerBudget)
	}
}

// TestDynamicProxyTransport_CachesByEndpoint 验证 transport 按 endpoint 缓存复用。
func TestDynamicProxyTransport_CachesByEndpoint(t *testing.T) {
	tr := &dynamicProxyTransport{
		base:       defaultBaseTransport(),
		byEndpoint: make(map[string]*http.Transport),
	}

	a1 := tr.transportFor("http://127.0.0.1:7897")
	a2 := tr.transportFor("http://127.0.0.1:7897")
	if a1 != a2 {
		t.Error("same endpoint should reuse the same transport pointer")
	}
	if a1.Proxy == nil {
		t.Error("proxy endpoint should set Proxy on transport")
	}

	b := tr.transportFor("http://127.0.0.1:7898")
	if a1 == b {
		t.Error("different endpoint should get a different transport")
	}

	n1 := tr.transportFor("")
	n2 := tr.transportFor("")
	if n1 != n2 {
		t.Error("no-proxy endpoint should also be cached")
	}
	if n1.Proxy != nil {
		t.Error("empty endpoint should have Proxy nil")
	}
	if a1 == n1 {
		t.Error("proxy and no-proxy transports must differ")
	}

	bad1 := tr.transportFor("://not-a-url")
	bad2 := tr.transportFor("://not-a-url")
	if bad1 != bad2 || bad1.Proxy != nil {
		t.Error("invalid endpoint should be cached with Proxy nil")
	}
}

// TestDynamicProxyTransport_RoundTripReusesConnection 验证连续 RoundTrip
// 复用同一 transport（连接池不丢）：第二次请求不再 dial；resolver 仍每次调用。
func TestDynamicProxyTransport_RoundTripReusesConnection(t *testing.T) {
	base := defaultBaseTransport()
	var dials int32
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&dials, 1)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}

	var resolverCalls int32
	tr := &dynamicProxyTransport{
		resolver: func() string {
			atomic.AddInt32(&resolverCalls, 1)
			return "" // 无代理
		},
		base:       base,
		byEndpoint: make(map[string]*http.Transport),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip #%d failed: %v", i+1, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	if got := atomic.LoadInt32(&dials); got != 1 {
		t.Errorf("dials = %d, want 1 (transport 应缓存复用连接池，而非每请求 Clone)", got)
	}
	if got := atomic.LoadInt32(&resolverCalls); got != 2 {
		t.Errorf("resolver calls = %d, want 2 (每次请求仍应动态解析 endpoint)", got)
	}
}
