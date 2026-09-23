package client

import (
	"time"

	"resty.dev/v3"
	"websearch/pkg/config"
	"websearch/pkg/proxy"
)

// DefaultTimeout 上游 API 请求默认超时时间（30s）。
// 覆盖单次 API RTT；apipool 多 SK 最坏约 N*30s，可接受。
const DefaultTimeout = 30 * time.Second

// New 创建带超时的 resty 客户端，默认强制直连。
// resty 默认 transport 是 ProxyFromEnvironment，会被 HTTP(S)_PROXY 等环境
// 变量静默劫持；这里显式置空 Proxy，供应商请求不受本机代理环境影响。
// 需要代理时由 newDefaultClient 按 proxy.api_providers 显式启用。
// timeout 可注入（测试用 200ms 等短值）。
func New(timeout time.Duration) *resty.Client {
	return resty.New().SetTimeout(timeout).SetTransport(proxy.NewUpstreamTransport(nil))
}

// DefaultClient 全局共享客户端，所有 API 适配器（baidu/tavily/exa/llm 等）使用。
// 超时来自配置 upstream_timeout_sec：默认 30s，显式 0 = 不设超时（有挂起风险）；
// proxy.api_providers: true 时按引擎层同一套代理解析走代理，默认直连。
var DefaultClient = newDefaultClient()

// newDefaultClient 按配置初始化全局客户端。
// 配置加载失败时回退默认 30s 直连。
func newDefaultClient() *resty.Client {
	conf, err := config.LoadOrDefault("")
	if err != nil {
		return New(DefaultTimeout)
	}
	c := resty.New().SetTransport(proxy.NewUpstreamTransport(conf.Proxy.UpstreamResolver()))
	if conf.UpstreamTimeoutSec > 0 {
		c.SetTimeout(time.Duration(conf.UpstreamTimeoutSec) * time.Second)
	}
	return c
}
