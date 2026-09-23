package dashboard

import "websearch/pkg/config"

// MCPToolView 是控制中心展示的公开 MCP 工具清单条目。它与
// telemetry.Overview.Tools 不同：后者只包含产生过真实调用的事件聚合。
type MCPToolView struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

// mcpToolCatalog 定义控制中心固定展示的公开 MCP 工具。Enabled 只反映当前
// 配置开关，不表示已经产生过真实调用；观测状态由遥测事件补充。
func mcpToolCatalog(conf config.Config) []MCPToolView {
	return []MCPToolView{
		{
			Name:        "smartsearch",
			Label:       "智能搜索",
			Enabled:     conf.Bing.Enabled,
			Description: "通用联网检索工具",
		},
		{
			Name:        "academicsearch",
			Label:       "学术检索",
			Enabled:     conf.Academic.Enabled,
			Description: "多学术数据库并行检索",
		},
		{
			Name:        "cleanfetch",
			Label:       "网页抓取",
			Enabled:     conf.CleanFetch.Enabled,
			Description: "网页正文抓取与 Markdown 提取",
		},
		{
			Name:        "pdf_parser",
			Label:       "PDF 解析",
			Enabled:     conf.PDFParser.Enabled,
			Description: "PDF 文本提取与 MinerU 回退",
		},
	}
}
