package antirobot

import (
	"net/url"
	"sort"
	"strings"
)

// trackingParams 去重键剥离的追踪参数白名单：前缀 utm_ 与精确名。
// 保守取交集：只剥离确定与内容无关的营销参数，宁漏合不误合。
var trackingParams = map[string]struct{}{
	"fbclid": {},
	"gclid":  {},
	"ref":    {},
	"spm":    {},
}

// NormalizeURL 归一化 URL 作为去重键（与 NormalizeDOI 同层，供各搜索层共用）：
// scheme/host 小写、去 fragment、剥离追踪参数、查询参数排序、非根路径去尾斜杠。
// 解析失败或无 host 时回退 TrimSpace 原文。仅用于去重键，展示仍用原始 URL。
func NormalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}

	var b strings.Builder
	b.WriteString(strings.ToLower(u.Scheme))
	b.WriteString("://")
	b.WriteString(strings.ToLower(u.Host))
	path := u.EscapedPath()
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}
	b.WriteString(path)

	if q := normalizeQuery(u.Query()); q != "" {
		b.WriteString("?")
		b.WriteString(q)
	}
	return b.String()
}

// normalizeQuery 剥离追踪参数并按 key 排序，保证同参数不同顺序得到同一键。
func normalizeQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for k, vs := range values {
		if isTrackingParam(k) {
			continue
		}
		if len(vs) == 0 {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("&")
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteString("=")
		b.WriteString(url.QueryEscape(values[k][0]))
	}
	return b.String()
}

func isTrackingParam(key string) bool {
	k := strings.ToLower(key)
	if strings.HasPrefix(k, "utm_") {
		return true
	}
	_, ok := trackingParams[k]
	return ok
}
