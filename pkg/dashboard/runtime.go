package dashboard

import (
	"time"

	mcpserver "websearch/mcp"
	"websearch/pkg/config"
)

type runtimeView struct {
	StartedAt      time.Time  `json:"started_at"`
	Version        string     `json:"version"`
	UptimeSeconds  int64      `json:"uptime_seconds"`
	MCPStateless   bool       `json:"mcp_stateless"`
	MCPResources   bool       `json:"mcp_resources"`
	SearXNGEnabled bool       `json:"searxng_enabled"`
	Proxy          proxyView  `json:"proxy"`
	LLM            llmView    `json:"llm"`
	MCPHub         mcpHubView `json:"mcp_hub"`
}

type proxyView struct {
	Mode         string `json:"mode"`
	Resolved     bool   `json:"resolved"`
	Endpoint     string `json:"endpoint,omitempty"`
	APIProviders bool   `json:"api_providers"`
}

type llmView struct {
	Enabled        bool   `json:"enabled"`
	ModelID        string `json:"model_id"`
	TimeoutSeconds int64  `json:"timeout_seconds"`
}

type mcpHubView struct {
	Enabled   bool               `json:"enabled"`
	FetchMode string             `json:"fetch_mode"`
	Servers   []mcpHubServerView `json:"servers"`
}

type mcpHubServerView struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func buildRuntimeView(conf config.Config) runtimeView {
	version, uptime := mcpserver.ProcessInfo()
	proxyMode := "auto"
	switch {
	case !conf.Proxy.NeedsProxy():
		proxyMode = "disabled"
	case conf.Proxy.Enabled:
		proxyMode = "explicit"
	}

	configuredServers := conf.MCPHub.GetServers()
	servers := make([]mcpHubServerView, 0, len(configuredServers))
	for _, server := range configuredServers {
		servers = append(servers, mcpHubServerView{Name: server.Name, Enabled: server.IsEnabled()})
	}
	proxyEndpoint := conf.Proxy.GetProxyEndpoint()
	_, displayEndpoint := diagnosticEndpoint(proxyEndpoint)
	return runtimeView{
		StartedAt: mcpserver.ProcessStartedAt().UTC(), Version: version, UptimeSeconds: int64(uptime.Seconds()),
		MCPStateless: conf.MCPStateless, MCPResources: conf.ResourcesEnabled(),
		// The compatibility route was removed upstream and has no enable switch.
		SearXNGEnabled: false,
		Proxy:          proxyView{Mode: proxyMode, Resolved: proxyEndpoint != "", Endpoint: displayEndpoint, APIProviders: conf.Proxy.APIProviders},
		LLM: llmView{Enabled: conf.LLMEnabled(), ModelID: conf.LLM.ModelId,
			TimeoutSeconds: int64(conf.LLM.GetTimeout().Seconds())},
		MCPHub: mcpHubView{Enabled: conf.MCPHub.IsEnabled(), FetchMode: conf.MCPHub.GetFetchMode(), Servers: servers},
	}
}
