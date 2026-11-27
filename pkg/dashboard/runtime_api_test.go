package dashboard

import (
	"bytes"
	"encoding/json"
	"testing"

	"websearch/pkg/config"
)

func TestOverviewRuntimeSnapshot(t *testing.T) {
	h, mux := newTestHandler(t)
	enabled := true
	h.conf.MCPStateless = true
	h.conf.Proxy = config.ProxyConfig{Enabled: true, Endpoint: "http://user:fixture-proxy-secret@localhost:9999/fixture-proxy-path?token=fixture-proxy-query", APIProviders: true}
	h.conf.LLM = config.LLMConfig{BaseURL: "https://fixture-private-llm", APIKey: "fixture-llm-secret", ModelId: "fixture-model", TimeoutSec: 37}
	h.conf.MCPHub = config.MCPHubConfig{Enabled: &enabled, FetchMode: "prefer", Servers: []config.MCPHubServerConfig{{Name: "parallel", APIKey: "fixture-hub-secret"}}}

	raw := getJSON(t, mux, "/__admin/api/overview")
	var got struct {
		Runtime struct {
			Version       string `json:"version"`
			UptimeSeconds *int64 `json:"uptime_seconds"`
			MCPStateless  bool   `json:"mcp_stateless"`
			MCPResources  bool   `json:"mcp_resources"`
			SearXNG       *bool  `json:"searxng_enabled"`
			Proxy         struct {
				Mode         string `json:"mode"`
				Resolved     bool   `json:"resolved"`
				Endpoint     string `json:"endpoint"`
				APIProviders bool   `json:"api_providers"`
			} `json:"proxy"`
			LLM struct {
				Enabled bool   `json:"enabled"`
				Model   string `json:"model_id"`
				Timeout int64  `json:"timeout_seconds"`
			} `json:"llm"`
			MCPHub struct {
				Enabled   bool   `json:"enabled"`
				FetchMode string `json:"fetch_mode"`
				Servers   []struct {
					Name    string `json:"name"`
					Enabled bool   `json:"enabled"`
				} `json:"servers"`
			} `json:"mcp_hub"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	r := got.Runtime
	if r.Version == "" || r.UptimeSeconds == nil || *r.UptimeSeconds < 0 || !r.MCPStateless || !r.MCPResources || r.SearXNG == nil || *r.SearXNG {
		t.Fatalf("unexpected process or MCP runtime: %+v", r)
	}
	if r.Proxy.Mode != "explicit" || !r.Proxy.Resolved || r.Proxy.Endpoint != "http://localhost:9999" || !r.Proxy.APIProviders {
		t.Fatalf("unexpected proxy snapshot: %+v", r.Proxy)
	}
	if !r.LLM.Enabled || r.LLM.Model != "fixture-model" || r.LLM.Timeout != 37 {
		t.Fatalf("unexpected LLM snapshot: %+v", r.LLM)
	}
	if !r.MCPHub.Enabled || r.MCPHub.FetchMode != "prefer" || len(r.MCPHub.Servers) != 1 || r.MCPHub.Servers[0].Name != "parallel" || !r.MCPHub.Servers[0].Enabled {
		t.Fatalf("unexpected MCPHub snapshot: %+v", r.MCPHub)
	}
	for _, sensitive := range []string{"fixture-proxy-secret", "fixture-proxy-path", "fixture-proxy-query", "fixture-private-llm", "fixture-llm-secret", "fixture-hub-secret"} {
		if bytes.Contains(raw, []byte(sensitive)) {
			t.Fatal("overview exposed sensitive connection configuration")
		}
	}
}
