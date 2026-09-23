package telemetry

import (
	"regexp"
	"strings"
)

// Error kinds classify why a tool or provider call failed. They are derived
// from error text in the recording layer so every caller gets consistent
// semantics without extra plumbing.
const (
	ErrorKindNone         = ""
	ErrorKindRateLimit    = "rate_limit"
	ErrorKindCaptcha      = "captcha"
	ErrorKindAccessDenied = "access_denied"
	ErrorKindTimeout      = "timeout"
	ErrorKindNetwork      = "network"
	ErrorKindParse        = "parse"
	ErrorKindNoResult     = "no_result"
	ErrorKindUnknown      = "unknown"
)

var (
	reHTTPStatus   = regexp.MustCompile(`(?i)\b(?:http(?: status)?|status(?: code)?|http error)\D{0,3}(\d{3})\b`)
	reBareHTTPCode = regexp.MustCompile(`(?i)\bhttp\s*(?:error\s*)?(4\d{2}|5\d{2})\b`)
)

// ClassifyErrorKind returns a stable category for an error. Recording applies
// this automatically; callers may also invoke it directly.
func ClassifyErrorKind(err error) string {
	if err == nil {
		return ErrorKindNone
	}
	return classifyErrorMessage(strings.ToLower(err.Error()))
}

func classifyErrorMessage(msg string) string {
	msg = strings.TrimSpace(strings.ToLower(msg))
	if msg == "" {
		return ErrorKindUnknown
	}

	switch {
	case containsAny(msg, "captcha", "验证码", "人机验证", "challenge required", "anubis", "请进行验证", "安全验证"):
		return ErrorKindCaptcha
	case containsAny(msg, "no text extracted", "scanned/image-based", "invalid character", "unmarshal", "json parse", "xml parse", "parse error", "解析失败", "解析异常", "html parse", "syntax error", "invalid json", "cannot unmarshal", "illegal character"):
		return ErrorKindParse
	case containsAny(msg, "no result", "no results", "no match", "搜索无结果", "无结果", "not found", "empty response", "empty result"):
		return ErrorKindNoResult
	case containsAny(msg, "too many requests", "rate limit", "rate-limited", "rate limited", "quota exceeded", "请求过于频繁", "频率过高", "限流", "限速", "请求过多", "超过配额"):
		return ErrorKindRateLimit
	case containsAny(msg, "forbidden", "access denied", "not authorized", "unauthorized", "拒绝访问", "访问被拒", "无权访问", "被拒绝"):
		return ErrorKindAccessDenied
	case containsAny(msg, "timeout", "timed out", "deadline exceeded", "超时"):
		return ErrorKindTimeout
	case containsAny(msg, "connection refused", "connection reset", "no such host", "dial tcp", "tls handshake", "network is unreachable", "connection closed", "broken pipe", "网络错误", "连接失败", "连接被重置", "bad gateway", "service unavailable"):
		return ErrorKindNetwork
	}

	switch code := statusCodeFromMessage(msg); {
	case code == 429:
		return ErrorKindRateLimit
	case code == 401 || code == 403:
		return ErrorKindAccessDenied
	case code >= 500:
		return ErrorKindNetwork
	}
	return ErrorKindUnknown
}

func statusCodeFromMessage(msg string) int {
	if match := reHTTPStatus.FindStringSubmatch(msg); len(match) == 2 {
		return atoiSafe(match[1])
	}
	if match := reBareHTTPCode.FindStringSubmatch(msg); len(match) == 2 {
		return atoiSafe(match[1])
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
