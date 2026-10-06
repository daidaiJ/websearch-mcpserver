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
		Description: "服务器能力矩阵：已注册工具、网页引擎、API 供应商（仅 Key 数量）与功能开关（只读，不含密钥）。",
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
		Description: "运行健康状态：引擎冷却/熔断（跨进程落盘共享）、进程运行时长（JSON，不含密钥）。",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      "search://health",
			MIMEType: "application/json",
			Text:     renderHealth(),
		}}}, nil
	})
	log.Info("Available resource: search://capabilities, search://health")
}

// renderCapabilities 从运行时注册事实渲染能力矩阵（markdown）。
// 密钥红线：API 供应商只输出已配置 Key 的数量，任何字段不得出现密钥值。
func renderCapabilities(conf config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# websearch-mcpserver\n\n")
	fmt.Fprintf(&b, "- 版本: %s\n", serverVersion)
	fmt.Fprintf(&b, "- 搜索模式: %s\n", conf.GetMode())

	b.WriteString("\n## 工具（已注册）\n\n")
	anyTool := false
	if conf.Bing.Enabled {
		anyTool = true
		b.WriteString("- `smartsearch`：通用联网检索，多引擎并发编排 + 失败清单透出；支持 intent 摘要与 fetch_top_n 正文补抓\n")
	}
	if conf.Academic.Enabled && academicSearcher != nil {
		anyTool = true
		fmt.Fprintf(&b, "- `academicsearch`：学术检索（引擎: %s）\n", strings.Join(academicSearcher.AcademicEngines(), " / "))
	}
	if conf.CleanFetch.Enabled {
		anyTool = true
		b.WriteString("- `cleanfetch`：网页干净 Markdown 抓取（SSRF 防护，支持批量）\n")
	}
	if conf.PDFParser.Enabled {
		anyTool = true
		b.WriteString("- `pdf_parser`：PDF 解析（本地库 + MinerU 增强回退）\n")
	}
	if !anyTool {
		b.WriteString("- （无已注册工具）\n")
	}

	b.WriteString("\n## 网页引擎（零 Key，兜底 Bing 恒可用）\n\n")
	fmt.Fprintf(&b, "- baidu_web: %v（默认关闭，实测被 CAPTCHA 识别）\n", conf.Baidu.WebEnabled)
	fmt.Fprintf(&b, "- so360: %v（国内直连可用）\n", conf.So360.Enabled)
	fmt.Fprintf(&b, "- wikipedia: %v（需代理）\n", conf.Wikipedia.Enabled)
	fmt.Fprintf(&b, "- googlenews: %v（需代理，跳转链接回源发布方）\n", conf.GoogleNews.Enabled)
	fmt.Fprintf(&b, "- google: %v（默认关闭，JS 挑战无法伪装绕过）\n", conf.Google.Enabled)
	fmt.Fprintf(&b, "- duckduckgo: %v\n", conf.DuckDuckGo.Enabled)

	b.WriteString("\n## API 供应商（已配置 Key 数量，值永不回显）\n\n")
	fmt.Fprintf(&b, "- baidu qianfan: %d\n", len(conf.Baidu.EffectiveSKList()))
	fmt.Fprintf(&b, "- tavily: %d\n", len(conf.Tavily.EffectiveSKList()))
	fmt.Fprintf(&b, "- exa: %d\n", len(conf.Exa.EffectiveSKList()))
	fmt.Fprintf(&b, "- anysearch: %d\n", len(conf.Anysearch.EffectiveSKList()))
	fmt.Fprintf(&b, "- doubao: %d\n", len(conf.Doubao.EffectiveSKList()))

	b.WriteString("\n## 功能\n\n")
	fmt.Fprintf(&b, "- LLM 摘要: %v\n", conf.LLMEnabled())
	fmt.Fprintf(&b, "- SQLite 缓存: %v\n", conf.CacheEnabled())
	fmt.Fprintf(&b, "- fetch_top_n 默认: %d（0 = 仅标题摘要，agent 显式传参优先）\n", conf.SmartSearch.FetchTopN)
	fmt.Fprintf(&b, "- MinerU PDF 增强: %v（OCR 回退: %v）\n", conf.PDFParser.MinerUEnabled(), conf.PDFParser.MinerUOCREnabled())
	b.WriteString("- 引擎冷却落盘: engine_health.json（缓存库同目录，重启后继承冷却避免重撞死引擎）\n")
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
