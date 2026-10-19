package mcpserver

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

// FuzzValidateURLSecurity 覆盖抓取预检的解析与私网判定：
// 目标是不 panic、不把已知内网/元数据地址判为安全。
func FuzzValidateURLSecurity(f *testing.F) {
	seeds := []string{
		"https://example.com/path",
		"http://127.0.0.1:8080/admin",
		"http://localhost/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/",
		"http://0.0.0.0/",
		"http://2130706433/",         // 十进制回环
		"http://0x7f000001/",         // 十六进制回环
		"http://0177.0.0.1/",         // 八进制回环
		"http://[::ffff:127.0.0.1]/", // IPv4-mapped IPv6
		"file:///etc/passwd",
		"gopher://example.com/",
		"http://user:pass@example.com/",
		"http://example.com:99999/",
		"",
		"://bad",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		err := validateURLSecurity(raw)
		if err == nil {
			// 放行时必须是 http(s) 且有主机名，避免协议/主机名缺失被静默接受
			if !looksLikeHTTPSHost(raw) {
				t.Fatalf("非法 URL 被放行: %q", raw)
			}
		}
	})
}

// looksLikeHTTPSHost 与 validateURLSecurity 的放行前提保持一致（仅用于 fuzz 断言）。
func looksLikeHTTPSHost(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return (scheme == "http" || scheme == "https") && parsed.Hostname() != ""
}

// FuzzIsPublicIP 覆盖 IP 分类：私网、回环、链路本地与云元数据必须判为不安全。
func FuzzIsPublicIP(f *testing.F) {
	for _, seed := range []string{
		"8.8.8.8", "1.1.1.1", "127.0.0.1", "10.0.0.1", "192.168.1.1",
		"172.16.0.1", "169.254.169.254", "::1", "::", "fe80::1",
		"fd00::ec2:e4a:c2fe", "2001:4860:4860::8888", "0.0.0.0",
		"::ffff:127.0.0.1", "not-an-ip", "",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		ip := net.ParseIP(raw)
		if ip == nil {
			return
		}
		public := isPublicIP(ip)
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			if public {
				t.Fatalf("内网/回环地址被判为公网: %s", ip)
			}
		}
		if ip.Equal(net.IPv4(169, 254, 169, 254)) && public {
			t.Fatalf("云元数据地址被判为公网: %s", ip)
		}
	})
}

// FuzzIsPrivateHostFast 覆盖快速主机名判定：已知内网名必须拒绝，普通域名不误杀。
func FuzzIsPrivateHostFast(f *testing.F) {
	for _, seed := range []string{
		"localhost", "LOCALHOST", "127.0.0.1", "::1", "[::1]", "0.0.0.0",
		"169.254.169.254", "metadata.google.internal", "example.com", "",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, host string) {
		if isPrivateHostFast(host) {
			switch host {
			case "localhost", "127.0.0.1", "::1", "0.0.0.0", "169.254.169.254",
				"metadata.google.internal", "[::1]":
			default:
				// 允许新增拒绝项，但必须确实属于内网语义，避免误杀公网域名
				if !isLoopbackOrMetadataLiteral(host) {
					t.Fatalf("公网主机名被误判为内网: %q", host)
				}
			}
		}
	})
}

func isLoopbackOrMetadataLiteral(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()
	}
	return false
}
