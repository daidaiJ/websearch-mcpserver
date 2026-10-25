package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 运行状态 MCP Resource 化（P1-10）：health/capabilities 走 Resource 而非工具——
// 不占工具槽位，不增加每次工具选择需要阅读的 schema token；内容为只读观测事实，
// 结构上不含任何密钥（provider 只出现 Key 数量，永不出现值）。
// 可经 mcp_resources: false 整体关闭（config.ResourcesEnabled）。

var (
	versionOnce sync.Once
	// serverVersion 二进制版本，由入口 main 注入（-X main.version 的同值）。
	serverVersion = "dev"
	// processStarted 进程启动时刻，health 资源计算运行时长。
	processStarted = time.Now()
)

// SetServerVersion 注入二进制版本（入口 main 调用，首个非空值生效）。
func SetServerVersion(v string) {
	if v == "" {
		return
	}
	versionOnce.Do(func() { serverVersion = v })
}

// registerResources 注册只读观测 Resource（须在 NewMCPServer 中调用）。
func registerResources(server *mcp.Server, conf config.Config) {
	server.AddResource(&mcp.Resource{
		URI:         "search://capabilities",
		Name:        "capabilities",
		Description: i18n.T("mcp.resource.capabilities.desc"),
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      "search://capabilities",
			MIMEType: "text/markdown",
			Text:     renderCapabilities(conf),
		}}}, nil
	})

	server.AddResource(&mcp.Resource{
		URI:         "search://health",
		Name:        "health",
		Description: i18n.T("mcp.resource.health.desc"),
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      "search://health",
			MIMEType: "application/json",
			Text:     renderHealth(),
		}}}, nil
	})
	log.InfoT("log.mcp.available_resource_search_capabilities_search_health")
}

// renderCapabilities 从运行时注册事实渲染能力矩阵（markdown）。
// 密钥红线：API 供应商只输出已配置 Key 的数量，任何字段不得出现密钥值。
func renderCapabilities(conf config.Config) string {
	var b strings.Builder
	b.WriteString("# websearch-mcpserver\n\n")
	b.WriteString(i18n.T("mcp.cap.version", "Version", serverVersion))
	b.WriteString(i18n.T("mcp.cap.mode", "Mode", conf.GetMode()))

	b.WriteString(i18n.T("mcp.cap.tools_head"))
	registered := make(map[string]bool)
	for _, state := range ToolSnapshot() {
		registered[state.Name] = state.Registered
	}
	anyTool := false
	if registered["smartsearch"] {
		anyTool = true
		b.WriteString(i18n.T("mcp.cap.tool.smartsearch"))
	}
	if registered["academicsearch"] && academicSearcher != nil {
		anyTool = true
		b.WriteString(i18n.T("mcp.cap.tool.academicsearch",
			"Engines", strings.Join(academicSearcher.AcademicEngines(), " / ")))
	}
	if registered["cleanfetch"] {
		anyTool = true
		b.WriteString(i18n.T("mcp.cap.tool.cleanfetch", "Enabled", conf.CleanFetch.Enabled))
	}
	if registered["pdf_parser"] {
		anyTool = true
		b.WriteString(i18n.T("mcp.cap.tool.pdf_parser"))
	}
	if registered["file_search"] {
		anyTool = true
		b.WriteString(i18n.T("mcp.cap.tool.file_search"))
	}
	if !anyTool {
		b.WriteString(i18n.T("mcp.cap.tools_none"))
	}

	b.WriteString(i18n.T("mcp.cap.engines_head"))
	fmt.Fprintf(&b, "- bing: %v\n", conf.Bing.Enabled)
	b.WriteString(i18n.T("mcp.cap.engine.baidu_web", "Enabled", conf.Baidu.WebEnabled))
	b.WriteString(i18n.T("mcp.cap.engine.so360", "Enabled", conf.So360.Enabled))
	b.WriteString(i18n.T("mcp.cap.engine.wikipedia", "Enabled", conf.Wikipedia.Enabled))
	b.WriteString(i18n.T("mcp.cap.engine.googlenews", "Enabled", conf.GoogleNews.Enabled))
	b.WriteString(i18n.T("mcp.cap.engine.google", "Enabled", conf.Google.Enabled))
	fmt.Fprintf(&b, "- duckduckgo: %v\n", conf.DuckDuckGo.Enabled)

	b.WriteString(i18n.T("mcp.cap.providers_head"))
	fmt.Fprintf(&b, "- baidu qianfan: %d\n", len(conf.Baidu.EffectiveSKList()))
	fmt.Fprintf(&b, "- tavily: %d\n", len(conf.Tavily.EffectiveSKList()))
	fmt.Fprintf(&b, "- exa: %d\n", len(conf.Exa.EffectiveSKList()))
	fmt.Fprintf(&b, "- anysearch: %d\n", len(conf.Anysearch.EffectiveSKList()))
	fmt.Fprintf(&b, "- doubao: %d\n", len(conf.Doubao.EffectiveSKList()))

	b.WriteString(i18n.T("mcp.cap.features_head"))
	b.WriteString(i18n.T("mcp.cap.feature.llm", "Enabled", conf.LLMEnabled()))
	b.WriteString(i18n.T("mcp.cap.feature.cache", "Enabled", conf.CacheEnabled()))
	b.WriteString(i18n.T("mcp.cap.feature.fetch_top_n", "Default", conf.SmartSearch.FetchTopN))
	b.WriteString(i18n.T("mcp.cap.feature.mineru",
		"Enabled", conf.PDFParser.MinerUEnabled(), "OCR", conf.PDFParser.MinerUOCREnabled()))
	b.WriteString(i18n.T("mcp.cap.feature.engine_health"))
	return b.String()
}

// cooldownView 引擎冷却观测视图（health 资源输出行）。
type cooldownView struct {
	Engine           string `json:"engine"`
	Kind             string `json:"kind,omitempty"`
	Reason           string `json:"reason,omitempty"`
	RemainingSeconds int64  `json:"remaining_seconds"`
	CooldownSeconds  int64  `json:"cooldown_seconds"`
	Until            string `json:"until"` // RFC3339
}

// healthPayload health 资源顶层结构。字段白名单式：无密钥、无查询内容。
type healthPayload struct {
	Version         string         `json:"version"`
	UptimeSeconds   int64          `json:"uptime_seconds"`
	EngineCooldowns []cooldownView `json:"engine_cooldowns"`
	CooldownPersist bool           `json:"cooldown_persist_enabled"`
}

// renderHealth 渲染 health 资源 JSON：引擎冷却/熔断状态 + 进程运行时长。
// 冷却快照来自 antirobot 注册表（跨进程落盘共享的活跃条目）。
func renderHealth() string {
	payload := healthPayload{
		Version:         serverVersion,
		UptimeSeconds:   int64(time.Since(processStarted).Seconds()),
		EngineCooldowns: []cooldownView{},
		CooldownPersist: antirobot.HealthPersistEnabled(),
	}
	for _, e := range antirobot.HealthSnapshot() {
		until := time.UnixMilli(int64(e.Until * 1000))
		payload.EngineCooldowns = append(payload.EngineCooldowns, cooldownView{
			Engine:           e.Engine,
			Kind:             e.Kind,
			Reason:           e.Reason,
			RemainingSeconds: int64(time.Until(until).Seconds()),
			CooldownSeconds:  int64(e.Cooldown),
			Until:            until.Format(time.RFC3339),
		})
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(data)
}
