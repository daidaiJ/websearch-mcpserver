package mcpserver

import (
	"context"
	"crypto/tls"
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

	"websearch/pkg/i18n"
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
		return fmt.Errorf("%s: %w", i18n.T("tool.security.url_malformed"), err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New(i18n.T("tool.security.scheme_unsupported", "Scheme", scheme))
	}

	host := parsed.Hostname()
	if host == "" {
		return errors.New(i18n.T("tool.security.host_missing"))
	}

	// 已知内网主机名直接拒绝
	if isPrivateHostFast(host) {
		return errors.New(i18n.T("tool.security.private_host", "Host", host))
	}

	// DNS 解析后检查 IP（防 DNS rebinding）
	ips, err := net.LookupHost(host)
	if err != nil {
		// DNS 解析失败不阻断（可能是临时 DNS 问题，由后续 fetch 报具体错误）
		log.InfoT("log.mcp.dns_resolution_failed_skipping_security_checks", "host", host, "err", err)
		return nil
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if !isPublicIP(ip) {
			return errors.New(i18n.T("tool.security.private_ip", "Host", host, "IP", ipStr))
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
	addrs, err := publicDownloadIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	for _, addr := range addrs {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New(i18n.T("tool.security.download_dial_failed", "Host", host))
}

func publicDownloadIPs(ctx context.Context, host string) ([]net.IPAddr, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, errors.New(i18n.T("tool.security.download_resolve_failed", "Host", host))
	}
	for _, addr := range addrs {
		if !isPublicIP(addr.IP) {
			return nil, errors.New(i18n.T("tool.security.private_ip_connect", "Host", host, "IP", addr.IP.String()))
		}
	}
	return addrs, nil
}

type pdfDownloadTransport struct {
	proxy func(*http.Request) (*url.URL, error)
}

func (t pdfDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := &http.Transport{DialContext: cropDialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	if t.proxy != nil {
		proxyURL, err := t.proxy(req)
		if err != nil {
			return nil, err
		}
		if proxyURL != nil {
			switch proxyURL.Scheme {
			case "http", "https", "socks5", "socks5h":
			default:
				return nil, errors.New(i18n.T("tool.security.pdf_proxy_scheme"))
			}
			if proxyURL.Hostname() == "" {
				return nil, errors.New(i18n.T("tool.security.pdf_proxy_host_missing"))
			}
			addrs, err := publicDownloadIPs(req.Context(), req.URL.Hostname())
			if err != nil {
				return nil, err
			}
			port := req.URL.Port()
			if port == "" {
				port = "80"
				if req.URL.Scheme == "https" {
					port = "443"
				}
			}
			// Pin the target even through a trusted proxy; retain the origin Host and TLS identity.
			pinned := req.Clone(req.Context())
			if pinned.Host == "" {
				pinned.Host = req.URL.Host
			}
			pinned.URL.Host = net.JoinHostPort(addrs[0].IP.String(), port)
			if req.URL.Scheme == "http" && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") {
				path := req.URL.EscapedPath()
				if path == "" {
					path = "/"
				}
				// Opaque keeps Request.WriteProxy from replacing the pinned URI authority with Host.
				pinned.URL.Opaque = "//" + pinned.URL.Host + path
			}
			transport.Proxy = http.ProxyURL(proxyURL)
			transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
			transport.TLSClientConfig = &tls.Config{ServerName: req.URL.Hostname()}
			if proxyURL.Scheme == "https" {
				transport.DialTLSContext = (&tls.Dialer{
					NetDialer: &net.Dialer{Timeout: 30 * time.Second},
					Config:    &tls.Config{ServerName: proxyURL.Hostname()},
				}).DialContext
			}
			return transport.RoundTrip(pinned)
		}
	}
	return transport.RoundTrip(req)
}

func pdfHTTPClient(timeout time.Duration) *http.Client {
	transport := pdfDownloadTransport{}
	if webfetchLazyCfg != nil {
		endpoint := nativeFetchProxy(*webfetchLazyCfg)
		if endpoint != "" {
			transport.proxy = func(*http.Request) (*url.URL, error) {
				u, err := url.Parse(endpoint)
				if err != nil {
					return nil, errors.New(i18n.T("tool.security.pdf_proxy_malformed"))
				}
				return u, nil
			}
		} else if webfetchLazyCfg.CleanFetch.UseSystemProxy {
			transport.proxy = http.ProxyFromEnvironment
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirectHops {
				return errors.New(i18n.T("tool.security.redirect_hops_exceeded", "Max", maxRedirectHops))
			}
			if err := validateURLSecurity(req.URL.String()); err != nil {
				return &unsafeRedirectError{cause: err}
			}
			return nil
		},
	}
}

func normalizePDFDownloadURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, "github.com") || u.User != nil {
		return rawURL
	}
	parts := strings.Split(u.EscapedPath(), "/")
	if len(parts) < 6 || parts[1] == "" || parts[2] == "" || parts[3] != "blob" || parts[4] == "" || parts[5] == "" {
		return rawURL
	}
	u.Host = "raw.githubusercontent.com"
	parts = append(parts[:3], parts[4:]...)
	u.RawPath = strings.Join(parts, "/")
	u.Path, _ = url.PathUnescape(u.RawPath)
	return u.String()
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
// 它是稳定哨兵（errors.Is 判定用），对外文案由 unsafeRedirectError 按语言渲染。
var errUnsafeRedirect = errors.New("unsafe_redirect")

// unsafeRedirectError 把哨兵身份与本地化文案分开：Unwrap 给出 errUnsafeRedirect，
// Error 给出当前语言的可读描述。
type unsafeRedirectError struct{ cause error }

func (e *unsafeRedirectError) Error() string {
	return i18n.T("tool.security.unsafe_redirect", "Err", e.cause.Error())
}

func (e *unsafeRedirectError) Unwrap() error { return errUnsafeRedirect }

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

	client := pdfHTTPClient(5 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errUnsafeRedirect) {
			return fmt.Errorf("%s: %w", i18n.T("tool.security.head_precheck_rejected"), err)
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
			return errors.New(i18n.T("tool.security.file_too_large",
				"Size", fmt.Sprintf("%.1f", float64(size)/1024/1024), "Limit", maxSizeMB))
		}
	}
	return nil
}

// downloadPDFForCrop keeps the original file local only until qpdf selects pages.
func downloadPDFForCrop(ctx context.Context, rawURL string) (string, func(), error) {
	return downloadPDFWithLimit(ctx, rawURL, 200*1024*1024)
}

func downloadPDFWithLimit(ctx context.Context, rawURL string, maxBytes int64) (string, func(), error) {
	rawURL = normalizePDFDownloadURL(rawURL)
	if err := validateURLSecurity(rawURL); err != nil {
		return "", nil, err
	}
	client := pdfHTTPClient(2 * time.Minute)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", i18n.T("tool.security.pdf_download_failed"), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, errors.New(i18n.T("tool.security.pdf_download_failed_http", "Status", resp.StatusCode))
	}
	if resp.ContentLength > maxBytes {
		return "", nil, errors.New(i18n.T("tool.security.pdf_over_download_limit", "Limit", maxBytes/(1024*1024)))
	}
	return saveLimitedPDF(resp.Body, maxBytes)
}

func saveLimitedPDF(body io.Reader, maxBytes int64) (string, func(), error) {
	file, err := os.CreateTemp("", "websearch-pdf-*.pdf")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	n, copyErr := io.Copy(file, io.LimitReader(body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n > maxBytes {
		cleanup()
		if n > maxBytes {
			return "", nil, errors.New(i18n.T("tool.security.pdf_over_download_limit", "Limit", maxBytes/(1024*1024)))
		}
		return "", nil, errors.New(i18n.T("tool.security.pdf_save_failed", "Err", errors.Join(copyErr, closeErr)))
	}
	return file.Name(), cleanup, nil
}
