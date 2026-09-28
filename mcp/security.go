package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"websearch/pkg/log"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。

// 抓取类工具的 URL 安全预检（SSRF/DNS rebinding）与 HEAD 大小检查。
// ── CleanFetch 安全预检 ──────────────────────────────────────────────────────

// validateURLSecurity DNS rebinding 防护：解析域名并检查所有 IP 是否为内网地址。
// 与 go-webfetch 的 BlockPrivateIP 形成双重防护（MCP 层预检 + 库层连接时检查）。
func validateURLSecurity(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL 格式错误: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("不支持的协议: %s（仅支持 http/https）", scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}

	// 已知内网主机名直接拒绝
	if isPrivateHostFast(host) {
		return fmt.Errorf("不允许访问内网地址: %s", host)
	}

	// DNS 解析后检查 IP（防 DNS rebinding）
	ips, err := net.LookupHost(host)
	if err != nil {
		// DNS 解析失败不阻断（可能是临时 DNS 问题，由后续 fetch 报具体错误）
		log.Infof("DNS 解析失败（跳过安全检查）: %s: %v", host, err)
		return nil
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if !isPublicIP(ip) {
			return fmt.Errorf("不允许访问内网地址: %s → %s", host, ipStr)
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !isCloudMetadata(ip)
}

// cropDialContext checks every DNS answer at connection time and dials a checked
// IP directly, so a hostname cannot resolve to a private address after preflight.
func cropDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("无法解析下载地址: %s", host)
	}
	for _, addr := range addrs {
		if !isPublicIP(addr.IP) {
			return nil, fmt.Errorf("不允许连接内网地址: %s → %s", host, addr.IP)
		}
	}
	var dialer net.Dialer
	for _, addr := range addrs {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("无法连接下载地址: %s", host)
}

// isPrivateHostFast 快速检查主机名是否为已知内网地址（无需 DNS 解析）。
func isPrivateHostFast(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0",
		"169.254.169.254", "metadata.google.internal":
		return true
	}
	// IPv6 回环
	if host == "[::1]" {
		return true
	}
	return false
}

// isCloudMetadata 检查 IP 是否为云厂商元数据地址。
func isCloudMetadata(ip net.IP) bool {
	// 169.254.169.254 (AWS/GCP/Azure/阿里云等)
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	// fd00::ec2:e4a:c2fe (AWS IPv6 元数据)
	if ip.IsLinkLocalUnicast() && ip.To4() == nil {
		return true
	}
	return false
}

// errUnsafeRedirect 标记重定向目标未通过私网/metadata 安全校验，
// 与普通网络错误区分：前者必须阻断，后者沿用"HEAD 失败不阻断"的宽松语义。
var errUnsafeRedirect = errors.New("不安全的重定向目标")

// maxRedirectHops HEAD 预检允许跟随的重定向跳数上限。
const maxRedirectHops = 5

// headCheck HEAD 预检：检查 Content-Length 防止下载过大文件。
// 重定向逐跳复跑 validateURLSecurity（F3）：公网 URL 302 到内网/云 metadata
// 时原实现会跟随并把 HEAD 打到链路本地地址。
func headCheck(ctx context.Context, rawURL string) error {
	maxSizeMB := cleanFetchMaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = 10
	}
	return headCheckWithLimit(ctx, rawURL, maxSizeMB)
}

func headCheckWithLimit(ctx context.Context, rawURL string, maxSizeMB int) error {
	req, err := http.NewRequestWithContext(ctx, "HEAD", rawURL, nil)
	if err != nil {
		return nil // URL 构造失败不阻断，由后续 fetch 报错
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirectHops {
				return fmt.Errorf("重定向跳数超过 %d", maxRedirectHops)
			}
			if err := validateURLSecurity(req.URL.String()); err != nil {
				return fmt.Errorf("%w: %v", errUnsafeRedirect, err)
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errUnsafeRedirect) {
			return fmt.Errorf("HEAD 预检拒绝: %v", err)
		}
		// 其它 HEAD 失败不阻断（某些服务器不支持 HEAD）
		return nil
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil // 状态码异常不阻断，由后续 fetch 报具体错误
	}

	if cl := resp.Header.Get("Content-Length"); cl != "" {
		size, err := strconv.ParseInt(cl, 10, 64)
		if err == nil && size > int64(maxSizeMB)*1024*1024 {
			return fmt.Errorf("文件过大（%.1fMB），超过限制（%dMB），如需抓取请调大 cleanfetch.max_fetch_size_mb",
				float64(size)/1024/1024, maxSizeMB)
		}
	}
	return nil
}

// downloadPDFForCrop keeps the original file local only until qpdf selects pages.
func downloadPDFForCrop(ctx context.Context, rawURL string) (string, func(), error) {
	const maxBytes = 200 * 1024 * 1024
	if err := validateURLSecurity(rawURL); err != nil {
		return "", nil, err
	}
	transport := &http.Transport{DialContext: cropDialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirectHops {
				return fmt.Errorf("重定向跳数超过 %d", maxRedirectHops)
			}
			return validateURLSecurity(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("下载待裁切 PDF 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("下载待裁切 PDF 失败: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return "", nil, fmt.Errorf("原 PDF 超过本地裁切下载上限 200MB")
	}
	file, err := os.CreateTemp("", "mineru-source-*.pdf")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	n, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n > maxBytes {
		cleanup()
		if n > maxBytes {
			return "", nil, fmt.Errorf("原 PDF 超过本地裁切下载上限 200MB")
		}
		return "", nil, fmt.Errorf("保存待裁切 PDF 失败: %v %v", copyErr, closeErr)
	}
	return file.Name(), cleanup, nil
}
