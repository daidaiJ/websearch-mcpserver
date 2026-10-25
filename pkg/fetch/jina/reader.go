package jina

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"websearch/pkg/config"
	fetchcontent "websearch/pkg/fetch"
	"websearch/pkg/log"
	"websearch/pkg/proxy"

	"resty.dev/v3"
)

const defaultBaseURL = "https://r.jina.ai"

type FetchResult struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	Content       string `json:"content"`
	Warning       string `json:"warning"`
	PublishedTime string `json:"publishedTime"`
	Usage         struct {
		Tokens int `json:"tokens"`
	} `json:"usage"`
}

type jinaResponse struct {
	Code   int         `json:"code"`
	Status int         `json:"status"`
	Data   FetchResult `json:"data"`
}

type Reader struct {
	apiKey  string
	baseURL string
	client  *resty.Client
}

func NewReader(apiKey, baseURL string, httpClient *http.Client) *Reader {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	var rc *resty.Client
	if httpClient != nil {
		rc = resty.NewWithClient(httpClient)
	} else {
		rc = resty.New()
	}
	return &Reader{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  rc,
	}
}

// NewFromConfig 根据配置创建 Jina Reader。
// 需要配置 API Key，否则返回 nil。
// 代理由 resolver 在请求时动态解析。
func NewFromConfig(jinaConf config.JinaConfig, proxyConf config.ProxyConfig) *Reader {
	if jinaConf.APIKey == "" {
		return nil
	}
	httpClient := proxy.NewDynamicHTTPClient(proxyConf.ProxyResolver(), 30*time.Second)
	return NewReader(jinaConf.APIKey, jinaConf.BaseURL, httpClient)
}

func (r *Reader) Fetch(rawURL string) (*FetchResult, error) {
	// 验证 URL 格式和协议
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效的 URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("不支持的协议: %s", parsed.Scheme)
	}
	// 拒绝内网地址
	host := parsed.Hostname()
	if isPrivateHost(host) {
		return nil, fmt.Errorf("不允许访问内网地址")
	}

	fetchURL := fmt.Sprintf("%s/%s", r.baseURL, rawURL)

	// 使用带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var resp jinaResponse
	res, err := r.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", r.apiKey)).
		SetHeader("X-Base", "final").
		SetHeader("X-Proxy", "auto").
		SetHeader("X-Retain-Images", "none").
		SetHeader("X-Respond-With", "content").
		SetHeader("X-Assert-Status-Code", "200").
		SetHeader("X-Timeout", "8").
		SetResult(&resp).
		Get(fetchURL)
	if err != nil {
		log.ErrorT("log.fetch.jina.jina_reader_request_failed", "error", err.Error())
		return nil, fmt.Errorf("jina reader 请求失败: %w", err)
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("jina reader 服务异常，HTTP %d", res.StatusCode())
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("目标页面: %s", describeHTTPError(resp.Code))
	}
	// Reader can return a successful envelope for a failed target response.
	if strings.HasPrefix(strings.TrimSpace(resp.Data.Warning), "Target URL returned error ") {
		return nil, fmt.Errorf("目标页面抓取失败: %s", resp.Data.Warning)
	}
	if !fetchcontent.HasPageBody(resp.Data.Content, resp.Data.Title) {
		return nil, fmt.Errorf("未获得页面正文(可能需要 JavaScript 渲染)")
	}
	return &resp.Data, nil
}

func describeHTTPError(code int) string {
	switch code {
	case 400:
		return "请求格式错误(400)"
	case 401:
		return "需要登录才能访问(401)"
	case 403:
		return "页面拒绝访问(403)"
	case 404:
		return "页面不存在(404)"
	case 408:
		return "抓取超时(408)"
	case 410:
		return "页面已被移除(410)"
	case 429:
		return "请求过于频繁，已被限流(429)"
	case 500, 502, 503:
		return fmt.Sprintf("目标服务器故障(%d)", code)
	default:
		return fmt.Sprintf("抓取失败，HTTP %d", code)
	}
}

// isPrivateHost 检测是否为内网地址（含 DNS 解析防 rebinding）。
func isPrivateHost(host string) bool {
	// 常见内网地址
	privateHosts := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"169.254.169.254",          // AWS 元数据
		"metadata.google.internal", // GCP 元数据
	}
	for _, ph := range privateHosts {
		if host == ph {
			return true
		}
	}
	// 检查私有 IP 段（主机名直接是 IP 的情况）
	if isPrivateIPString(host) {
		return true
	}
	// DNS 解析后检查 IP（防 DNS rebinding）
	ips, err := net.LookupHost(host)
	if err != nil {
		return false // DNS 失败不阻断，由后续请求报错
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()) {
			return true
		}
	}
	return false
}

// isPrivateIPString 检查字符串是否为私有/回环/链路本地 IP 地址。
// 使用 net.ParseIP + 标准库谓词，避免字符串前缀误伤公网段（如 172.32.0.0/11）。
// 与 isPrivateHost 中 DNS 分支的判定保持一致（fail-closed）。
func isPrivateIPString(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
