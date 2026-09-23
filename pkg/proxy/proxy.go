package proxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// ProxyResolver 代理端点动态解析函数。
// 返回当前生效的代理端点（如 "http://127.0.0.1:7897"），返回空字符串表示不走代理。
type ProxyResolver func() string

// dynamicProxyTransport 每次请求动态解析代理端点的 transport。
// 按「当前 endpoint 字符串」缓存 *http.Transport：endpoint 不变则复用（连接池不丢），
// endpoint 变化才新建；无代理（空串）也缓存一条 Proxy: nil 的 transport，禁止每请求 Clone。
type dynamicProxyTransport struct {
	resolver ProxyResolver
	base     *http.Transport

	mu         sync.Mutex
	byEndpoint map[string]*http.Transport
}

func (t *dynamicProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.resolver == nil {
		return t.base.RoundTrip(req)
	}
	return t.transportFor(t.resolver()).RoundTrip(req)
}

// transportFor 返回 endpoint 对应的 transport（缓存复用）。
// endpoint 为空或解析失败时按无代理处理，同样缓存。
func (t *dynamicProxyTransport) transportFor(endpoint string) *http.Transport {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byEndpoint == nil {
		t.byEndpoint = make(map[string]*http.Transport)
	}
	if tr, ok := t.byEndpoint[endpoint]; ok {
		return tr
	}
	tr := t.base.Clone()
	tr.Proxy = nil
	if endpoint != "" {
		if proxyURL, err := url.Parse(endpoint); err == nil {
			tr.Proxy = http.ProxyURL(proxyURL)
		}
	}
	t.byEndpoint[endpoint] = tr
	return tr
}

// defaultBaseTransport 返回带合理超时的默认 transport。
func defaultBaseTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
}

// NewHTTPClient 创建带代理的 HTTP 客户端（静态端点）。
// proxyEndpoint 为空时返回使用默认 transport 的客户端。
// 保留兼容旧调用方。
func NewHTTPClient(proxyEndpoint string, timeout time.Duration) *http.Client {
	if proxyEndpoint == "" {
		return &http.Client{Timeout: timeout}
	}
	proxyURL, err := url.Parse(proxyEndpoint)
	if err != nil {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}
}

// NewUpstreamTransport 返回 API 供应商上游请求使用的 transport。
// resolver 为 nil 时显式直连（Proxy 置 nil，不吃 HTTP(S)_PROXY 等环境变量，
// 默认行为）；非 nil 时每次请求动态解析代理端点，与引擎层同一套缓存逻辑。
func NewUpstreamTransport(resolver ProxyResolver) http.RoundTripper {
	if resolver == nil {
		return defaultBaseTransport()
	}
	return &dynamicProxyTransport{
		resolver:   resolver,
		base:       defaultBaseTransport(),
		byEndpoint: make(map[string]*http.Transport),
	}
}

// NewDynamicHTTPClient 创建动态代理 HTTP 客户端。
// 每次请求时通过 resolver 实时获取代理端点，支持运行时代理开关切换。
// resolver 为 nil 时返回无代理客户端。
// 自动处理 429 限流：读取 Retry-After 头等待后重试一次。
func NewDynamicHTTPClient(resolver ProxyResolver, timeout time.Duration) *http.Client {
	if resolver == nil {
		return &http.Client{Timeout: timeout, Transport: WithRetry(nil)}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: WithRetry(&dynamicProxyTransport{
			resolver:   resolver,
			base:       defaultBaseTransport(),
			byEndpoint: make(map[string]*http.Transport),
		}),
	}
}

// ── RetryAfter Transport ─────────────────────────────────────────────────────

// retryTransport 包装底层 transport，自动处理 429 + Retry-After 限流。
type retryTransport struct {
	inner   http.RoundTripper
	maxWait time.Duration // 最大重试等待时间，超过则直接返回 429
}

// WithRetry 为 transport 添加 429 + Retry-After 自动重试。
// maxWait 为最大等待时间（0 则默认 5s），超限则直接返回 429 不重试。
func WithRetry(inner http.RoundTripper, maxWait ...time.Duration) *retryTransport {
	mw := 5 * time.Second
	if len(maxWait) > 0 && maxWait[0] > 0 {
		mw = maxWait[0]
	}
	return &retryTransport{inner: inner, maxWait: mw}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}

	resp, err := inner.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusTooManyRequests {
		return resp, err
	}

	// 解析 Retry-After（支持秒数和 HTTP-Date）
	wait := 3 * time.Second
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, parseErr := strconv.Atoi(ra); parseErr == nil && secs > 0 {
			wait = time.Duration(secs) * time.Second
		} else if t, parseErr := http.ParseTime(ra); parseErr == nil {
			wait = time.Until(t)
			if wait < 0 {
				wait = 0
			}
		}
	}

	// 超过最大等待时间，直接返回 429 不重试
	if wait > t.maxWait {
		return resp, nil
	}

	resp.Body.Close()
	time.Sleep(wait)
	return inner.RoundTrip(req)
}
