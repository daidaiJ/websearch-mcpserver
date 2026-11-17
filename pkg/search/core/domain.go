package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

// Domain filter errors. Callers attach the offending pattern for user-facing
// messages; matching itself never fails.
var (
	ErrDomainPatternEmpty   = errors.New("empty domain pattern")
	ErrDomainPatternInvalid = errors.New("invalid domain pattern")
)

// DomainPatternError wraps an invalid domain pattern so callers can report the
// exact input without parsing the error text.
type DomainPatternError struct {
	Pattern string
	Reason  error
}

func (e *DomainPatternError) Error() string {
	return fmt.Sprintf("%v: %q", e.Reason, e.Pattern)
}

func (e *DomainPatternError) Unwrap() error { return e.Reason }

// DomainFilter 一次查询的域名白/黑名单。条目是归一化后的 host（小写、无端口、
// 无路径、无前导 www.），匹配目标 host 本身及其任意子域。Include 为非空时是硬
// 白名单；任一 Exclude 命中即丢弃，优先级高于 Include。
type DomainFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// Empty 表示不过滤。
func (f DomainFilter) Empty() bool { return len(f.Include) == 0 && len(f.Exclude) == 0 }

// Allows 判断结果 URL 是否通过域名过滤。无 host 的 URL 在存在 Include 时丢弃，
// 只有 Exclude 时保留（无法判断归属，不做无依据的排除）。
func (f DomainFilter) Allows(rawURL string) bool {
	if f.Empty() {
		return true
	}
	host := HostFromURL(rawURL)
	if host == "" {
		return len(f.Include) == 0
	}
	for _, pattern := range f.Exclude {
		if hostMatches(host, pattern) {
			return false
		}
	}
	if len(f.Include) == 0 {
		return true
	}
	for _, pattern := range f.Include {
		if hostMatches(host, pattern) {
			return true
		}
	}
	return false
}

// FilterByDomain 返回保留的结果与丢弃条数。无过滤或零丢弃时原样返回入参切片。
func FilterByDomain(results []SearchResult, f DomainFilter) ([]SearchResult, int) {
	if f.Empty() || len(results) == 0 {
		return results, 0
	}
	kept := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if f.Allows(r.Url) {
			kept = append(kept, r)
		}
	}
	dropped := len(results) - len(kept)
	if dropped == 0 {
		return results, 0
	}
	return kept, dropped
}

// NewDomainFilter 归一化并去重白/黑名单；顺序不敏感（排序后返回），保证缓存键
// 与匹配行为稳定。空字符串条目忽略。
func NewDomainFilter(include, exclude []string) (DomainFilter, error) {
	inc, err := NormalizeDomainPatterns(include)
	if err != nil {
		return DomainFilter{}, err
	}
	exc, err := NormalizeDomainPatterns(exclude)
	if err != nil {
		return DomainFilter{}, err
	}
	return DomainFilter{Include: inc, Exclude: exc}, nil
}

// NormalizeDomainPatterns 归一化并去重一组条目；空字符串忽略，返回结果排序。
func NormalizeDomainPatterns(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if strings.TrimSpace(item) == "" {
			continue
		}
		pattern, err := NormalizeDomainPattern(item)
		if err != nil {
			return nil, &DomainPatternError{Pattern: item, Reason: err}
		}
		if _, ok := seen[pattern]; ok {
			continue
		}
		seen[pattern] = struct{}{}
		out = append(out, pattern)
	}
	sort.Strings(out)
	return out, nil
}

// NormalizeDomainPattern 把常见输入收窄为可匹配的 host：
// example.com、www.example.com、*.example.com、example.com:8080、
// https://example.com/path 都归一化为 example.com；大小写与末尾点不敏感。
func NormalizeDomainPattern(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrDomainPatternEmpty
	}
	if strings.HasPrefix(s, "*.") {
		s = s[2:]
	} else if strings.Contains(s, "*") {
		return "", ErrDomainPatternInvalid
	}

	var host string
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", ErrDomainPatternInvalid
		}
		host = u.Hostname()
	} else {
		// 裸 host 可能带端口或路径；已带 // 前缀时按 network-path 直接解析。
		parseTarget := "//" + s
		if strings.HasPrefix(s, "//") {
			parseTarget = s
		}
		u, err := url.Parse(parseTarget)
		if err != nil {
			return "", ErrDomainPatternInvalid
		}
		host = u.Hostname()
	}

	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	host = strings.TrimPrefix(host, "www.")
	if host == "" ||
		strings.ContainsAny(host, " \t\r\n/\\?#@") ||
		strings.Contains(host, "..") ||
		strings.HasPrefix(host, ".") {
		return "", ErrDomainPatternInvalid
	}
	return host, nil
}

// HostFromURL 提取结果 URL 的 host（小写、去端口、去末尾点）。也接受
// protocol-relative 与裸 host 形式；无法解析时返回空串。
func HostFromURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		if strings.Contains(s, "://") {
			return ""
		}
		u, err = url.Parse("//" + s)
		if err != nil {
			return ""
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host
}

func hostMatches(host, pattern string) bool {
	if host == "" || pattern == "" {
		return false
	}
	if net.ParseIP(pattern) != nil {
		return host == pattern
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

// domainFilterKey 请求级域名过滤的 context 键：编排层（hybrid）在截断前读取，
// 避免跨请求共享可变过滤器。
type domainFilterKey struct{}

// WithDomainFilter 把本次请求的域名过滤下传给编排层。
func WithDomainFilter(ctx context.Context, f DomainFilter) context.Context {
	if f.Empty() {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, domainFilterKey{}, f)
}

// DomainFilterFromContext 读取请求级域名过滤；未设置返回空过滤器。
func DomainFilterFromContext(ctx context.Context) DomainFilter {
	if ctx == nil {
		return DomainFilter{}
	}
	f, _ := ctx.Value(domainFilterKey{}).(DomainFilter)
	return f
}
