package dashboard

import (
	mcpserver "websearch/mcp"
	"websearch/pkg/config"
	"websearch/pkg/i18n"
)

// MCPToolView 是控制中心展示的公开 MCP 工具清单条目。它与
// telemetry.Overview.Tools 不同：后者只包含产生过真实调用的事件聚合。
type MCPToolView struct {
	Name         string        `json:"name"`
	Label        string        `json:"label"`
	Enabled      bool          `json:"enabled"`
	Registered   bool          `json:"registered"`
	State        string        `json:"state"`
	Description  string        `json:"description"`
	FetchEnabled *bool         `json:"fetch_enabled,omitempty"`
	PDF          *PDFToolView  `json:"pdf,omitempty"`
	File         *FileToolView `json:"file,omitempty"`
}

type FileToolView struct {
	AllowedRootCount  int  `json:"allowed_root_count"`
	PathMetadataOnly  bool `json:"path_metadata_only"`
	WhitelistEnforced bool `json:"whitelist_enforced"`
}

type PDFToolView struct {
	MaxPages            int    `json:"max_pages"`
	MinerUPageLimit     int    `json:"mineru_page_limit"`
	MinerUPageBatchSize int    `json:"mineru_page_batch_size"`
	MinerUPageBudget    int    `json:"mineru_page_budget"`
	OCREnabled          bool   `json:"ocr_enabled"`
	PageProgress        string `json:"page_progress"`
}

// mcpToolCatalog 定义控制中心固定展示的公开 MCP 工具。Enabled 只反映当前
// 配置开关，不表示已经产生过真实调用；观测状态由遥测事件补充。
// loc 决定标签与描述的语言（控制中心按 Accept-Language 每请求协商）。
func mcpToolCatalog(conf config.Config, loc *i18n.Localizer) []MCPToolView {
	tools := []MCPToolView{
		{
			Name:        "smartsearch",
			Label:       loc.T("dashboard.tool.smartsearch.label"),
			Enabled:     len(activeSourceIDs(webSourceCatalog(conf, loc))) > 0,
			Description: loc.T("dashboard.tool.smartsearch.desc"),
		},
		{
			Name:        "academicsearch",
			Label:       loc.T("dashboard.tool.academicsearch.label"),
			Enabled:     conf.Academic.Enabled,
			Description: loc.T("dashboard.tool.academicsearch.desc"),
		},
		{
			Name:         "cleanfetch",
			Label:        loc.T("dashboard.tool.cleanfetch.label"),
			Enabled:      true,
			FetchEnabled: &conf.CleanFetch.Enabled,
			Description:  loc.T("dashboard.tool.cleanfetch.desc"),
		},
		{
			Name:        "pdf_parser",
			Label:       loc.T("dashboard.tool.pdf_parser.label"),
			Enabled:     conf.PDFParser.Enabled,
			Description: loc.T("dashboard.tool.pdf_parser.desc"),
			PDF: &PDFToolView{MaxPages: conf.PDFParser.GetMaxPages(),
				MinerUPageLimit:     conf.PDFParser.GetMinerUPageLimit(),
				MinerUPageBatchSize: conf.PDFParser.GetMinerUPageBatchSize(),
				MinerUPageBudget:    conf.PDFParser.GetMinerUPageBudget(),
				OCREnabled:          conf.PDFParser.MinerUOCREnabled(), PageProgress: "not_collected"},
		},
		{
			Name: "file_search", Label: loc.T("dashboard.tool.file_search.label"), Enabled: conf.Everything.URL != "",
			Description: loc.T("dashboard.tool.file_search.desc"),
			File: &FileToolView{AllowedRootCount: len(conf.Everything.Roots),
				PathMetadataOnly: true, WhitelistEnforced: len(conf.Everything.Roots) > 0},
		},
	}
	states := mcpserver.ToolSnapshot()
	for i := range tools {
		for _, state := range states {
			if tools[i].Name == state.Name {
				tools[i].State, tools[i].Registered = state.State, state.Registered
			}
		}
	}
	return tools
}
