package core

import "websearch/pkg/antirobot"

// NormalizeURL 返回搜索结果的去重键：scheme/host 小写、去 fragment、
// 剥离追踪参数（utm_*/fbclid/gclid/ref/spm）、查询参数排序、非根路径去尾斜杠。
// 规范化只作用于去重键，响应中展示的仍是原始 URL。
// 实现与 NormalizeDOI 同住 antirobot 层（core 依赖 antirobot，反向不可），此处转发。
func NormalizeURL(raw string) string {
	return antirobot.NormalizeURL(raw)
}
