package i18n

import "net/http"

// HTTP 把按 Accept-Language 协商出的 Localizer 注入请求 context。
// HTTP 面（控制中心、SearXNG 兼容端点）的语言由此每请求决定：
// 浏览器语言能被满足就用浏览器语言，否则回退进程默认（config.language 解析结果）。
func HTTP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		loc := For(r.Header.Get("Accept-Language"))
		next(w, r.WithContext(WithLocalizer(r.Context(), loc)))
	}
}
