package jina

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"websearch/pkg/client"
	"websearch/pkg/config"
	"websearch/pkg/log"
)

const defaultBaseURL = "https://r.jina.ai"

type FetchResult struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	Content       string `json:"content"`
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
}

func NewReader(apiKey, baseURL string) *Reader {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Reader{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// NewFromConfig 根据配置创建 Jina Reader，未配置 API Key 时返回 nil。
func NewFromConfig(conf config.JinaConfig) *Reader {
	if conf.APIKey == "" {
		return nil
	}
	return NewReader(conf.APIKey, conf.BaseURL)
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
	res, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", r.apiKey)).
		SetHeader("X-Base", "final").
		SetHeader("X-Proxy", "auto").
		SetHeader("X-Retain-Images", "none").
		SetHeader("X-Return-Format", "markdown").
		SetHeader("X-Timeout", "8").
		SetResult(&resp).
		Get(fetchURL)
	if err != nil {
		log.Errf("jina reader request failed: %s", err.Error())
		return nil, fmt.Errorf("jina reader 请求失败: %w", err)
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("jina reader 服务异常，HTTP %d", res.StatusCode())
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("目标页面: %s", describeHTTPError(resp.Code))
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

// isPrivateHost 检测是否为内网地址。
func isPrivateHost(host string) bool {
	// 常见内网地址
	privateHosts := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"169.254.169.254", // AWS 元数据
		"metadata.google.internal", // GCP 元数据
	}
	for _, ph := range privateHosts {
		if host == ph {
			return true
		}
	}
	// 检查私有 IP 段
	if strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "172.16.") ||
		strings.HasPrefix(host, "172.17.") ||
		strings.HasPrefix(host, "172.18.") ||
		strings.HasPrefix(host, "172.19.") ||
		strings.HasPrefix(host, "172.2") ||
		strings.HasPrefix(host, "172.3") ||
		strings.HasPrefix(host, "192.168.") {
		return true
	}
	return false
}
