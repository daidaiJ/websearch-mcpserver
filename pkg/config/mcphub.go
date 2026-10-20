package config

import (
	"strings"
	"time"
)

// MCPHub 级联抓取/搜索模式的默认值。
const (
	MCPHubFetchOff      = "off"      // 不启用 MCP 抓取增强（默认）
	MCPHubFetchFallback = "fallback" // 本地 webfetch/Jina 失败后再尝试 MCP
	MCPHubFetchPrefer   = "prefer"   // 优先尝试 MCP，失败后降级到本地 webfetch/Jina
)

// MCPHubConfig 零 Key 托管 MCP 级联 hub 配置。
// mode=mcphub 时作为搜索引擎；fetch_mode 非 off 时同时增强 cleanfetch。
type MCPHubConfig struct {
	Enabled         *bool                `mapstructure:"enabled"`           // 默认启用
	Strategy        string               `mapstructure:"strategy"`          // priority（默认）/ round-robin
	TimeoutSec      int                  `mapstructure:"timeout_sec"`       // 单次搜索超时，默认 20s
	FetchTimeoutSec int                  `mapstructure:"fetch_timeout_sec"` // 单次抓取超时，默认 45s
	CooldownSec     int                  `mapstructure:"cooldown_sec"`      // 失败冷却，默认 60s
	MaxResults      int                  `mapstructure:"max_results"`       // 默认 10
	MaxFetchChars   int                  `mapstructure:"max_fetch_chars"`   // 单个网页最多返回字符数，默认 200000
	FetchMode       string               `mapstructure:"fetch_mode"`        // off（默认）/ fallback / prefer
	RotationEngines []string             `mapstructure:"rotation_engines"`  // 加入主轮转池的本地引擎，分摊 MCP 限流
	FallbackEngines []string             `mapstructure:"fallback_engines"`  // 主轮转全部失败后的降级引擎；省略默认 bing → so360
	Servers         []MCPHubServerConfig `mapstructure:"servers"`           // 省略时使用 parallel → anysearch → exa → firecrawl
}

// MCPHubServerConfig 内置托管 MCP server。仅支持已知 preset
// （parallel / anysearch / exa / firecrawl）；请求参数与响应解析随 preset 固定，
// 新增上游必须在 mcphub 包内补 preset 与解析器，不能靠配置拼接。
type MCPHubServerConfig struct {
	Name    string   `mapstructure:"name"`
	Enabled *bool    `mapstructure:"enabled"`
	APIKey  string   `mapstructure:"api_key"`
	SKList  []string `mapstructure:"sk_list"`
	// RequiresProxy 控制是否只在检测到代理时启用该上游；nil = 使用预设默认值
	// （parallel/anysearch 直连，exa/firecrawl 默认需代理）。
	RequiresProxy *bool `mapstructure:"requires_proxy"`
}

// NormalizeMCPHub 填充 mcp_hub 的默认值；零值保持安全默认（fetch_mode=off）。
func (c *MCPHubConfig) NormalizeMCPHub() {
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = 20
	}
	if c.FetchTimeoutSec <= 0 {
		c.FetchTimeoutSec = 45
	}
	if c.CooldownSec <= 0 {
		c.CooldownSec = 60
	}
	if c.MaxResults <= 0 {
		c.MaxResults = 10
	}
	if c.MaxFetchChars <= 0 {
		c.MaxFetchChars = 200000
	}
	c.Strategy = c.GetStrategy()
	c.FetchMode = c.GetFetchMode()
}

// IsEnabled 返回 hub 总开关，默认开启。
func (c MCPHubConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// GetStrategy 返回级联起始策略，默认 priority。
func (c MCPHubConfig) GetStrategy() string {
	if strings.EqualFold(strings.TrimSpace(c.Strategy), "round-robin") {
		return "round-robin"
	}
	return "priority"
}

// GetFetchMode 归一化 cleanfetch 增强模式，默认 off。
func (c MCPHubConfig) GetFetchMode() string {
	switch strings.ToLower(strings.TrimSpace(c.FetchMode)) {
	case MCPHubFetchFallback:
		return MCPHubFetchFallback
	case MCPHubFetchPrefer:
		return MCPHubFetchPrefer
	default:
		return MCPHubFetchOff
	}
}

// GetTimeout 返回单次搜索超时。
func (c MCPHubConfig) GetTimeout() time.Duration {
	return time.Duration(c.TimeoutSec) * time.Second
}

// GetFetchTimeout 返回单次抓取超时。
func (c MCPHubConfig) GetFetchTimeout() time.Duration {
	return time.Duration(c.FetchTimeoutSec) * time.Second
}

// GetCooldown 返回失败冷却时间。
func (c MCPHubConfig) GetCooldown() time.Duration {
	return time.Duration(c.CooldownSec) * time.Second
}

// GetServers 返回按级联顺序排列的 server 列表。
// 省略时使用稳定且国内直连可用的四家：parallel → anysearch → exa → firecrawl。
func (c MCPHubConfig) GetServers() []MCPHubServerConfig {
	if len(c.Servers) == 0 {
		return []MCPHubServerConfig{
			{Name: "parallel"},
			{Name: "anysearch"},
			{Name: "exa"},
			{Name: "firecrawl"},
		}
	}
	out := make([]MCPHubServerConfig, 0, len(c.Servers))
	for _, s := range c.Servers {
		s.Name = strings.ToLower(strings.TrimSpace(s.Name))
		if s.Name == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// IsEnabled 返回单个 server 开关，默认开启。
func (s MCPHubServerConfig) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// GetRotationEngines 返回加入主轮转池的本地引擎名（小写、去空）。
func (c MCPHubConfig) GetRotationEngines() []string {
	return normalizeEngineNames(c.RotationEngines)
}

// GetFallbackEngines 返回主轮转全部失败后的降级引擎。
// 未配置（nil）时默认 bing → so360（必应接管百度网页的兜底位）；显式空数组
// 表示不启用本地降级，仍由 smartsearch 统一回退 Bing。
func (c MCPHubConfig) GetFallbackEngines() []string {
	if c.FallbackEngines == nil {
		return []string{"bing", "so360"}
	}
	return normalizeEngineNames(c.FallbackEngines)
}

func normalizeEngineNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// EffectiveKeys 返回合并后的 Key 列表：sk_list 优先，否则 api_key 单 key。
func (s MCPHubServerConfig) EffectiveKeys() []string {
	if len(s.SKList) > 0 {
		return s.SKList
	}
	if strings.TrimSpace(s.APIKey) != "" {
		return []string{s.APIKey}
	}
	return nil
}
