package config

import "testing"

func TestMCPHubDefaults(t *testing.T) {
	conf := Default()
	if conf.GetMode() != ModeEngine {
		t.Fatalf("default mode = %q", conf.GetMode())
	}
	if conf.NeedsAPIKey() {
		t.Fatal("engine mode must not require an API key")
	}
	if got := conf.MCPHub.GetStrategy(); got != "priority" {
		t.Fatalf("strategy = %q", got)
	}
	if got := conf.MCPHub.GetFetchMode(); got != MCPHubFetchOff {
		t.Fatalf("fetch mode = %q", got)
	}
	if conf.MCPHub.GetTimeout().Seconds() != 20 || conf.MCPHub.GetFetchTimeout().Seconds() != 45 {
		t.Fatalf("timeouts = %v/%v", conf.MCPHub.GetTimeout(), conf.MCPHub.GetFetchTimeout())
	}
	if got := conf.MCPHub.GetRotationEngines(); len(got) != 0 {
		t.Fatalf("rotation engines = %v", got)
	}
	if got := conf.MCPHub.GetFallbackEngines(); len(got) != 2 || got[0] != "bing" || got[1] != "so360" {
		t.Fatalf("fallback engines = %v", got)
	}
	names := make([]string, 0, 4)
	for _, s := range conf.MCPHub.GetServers() {
		names = append(names, s.Name)
	}
	want := []string{"parallel", "anysearch", "exa", "firecrawl"}
	if len(names) != len(want) {
		t.Fatalf("servers = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("servers = %v, want %v", names, want)
		}
	}
}

func TestMCPHubExplicitServersAndFetchMode(t *testing.T) {
	disabled := false
	cfg := MCPHubConfig{
		Strategy:  "round-robin",
		FetchMode: "PREFER",
		Servers: []MCPHubServerConfig{
			{Name: " Parallel "},
			{Name: "exa", Enabled: &disabled},
			{Name: ""},
		},
	}
	cfg.NormalizeMCPHub()
	if got := cfg.GetStrategy(); got != "round-robin" {
		t.Fatalf("strategy = %q", got)
	}
	if got := cfg.GetFetchMode(); got != MCPHubFetchPrefer {
		t.Fatalf("fetch mode = %q", got)
	}
	servers := cfg.GetServers()
	if len(servers) != 2 || servers[0].Name != "parallel" || servers[1].Name != "exa" {
		t.Fatalf("servers = %+v", servers)
	}
	if servers[1].IsEnabled() {
		t.Fatal("explicit disabled server must stay disabled")
	}
}

func TestMCPHubModeDoesNotNeedAPIKey(t *testing.T) {
	conf := Config{Mode: ModeMCPHub}
	if conf.GetMode() != ModeMCPHub {
		t.Fatalf("mode = %q", conf.GetMode())
	}
	if conf.NeedsAPIKey() {
		t.Fatal("mcphub is keyless by default")
	}
}

func TestMCPHubRotationAndFallbackEngines(t *testing.T) {
	cfg := MCPHubConfig{
		RotationEngines: []string{" SO360 ", "", "Bing"},
		FallbackEngines: []string{},
	}
	if got := cfg.GetRotationEngines(); len(got) != 2 || got[0] != "so360" || got[1] != "bing" {
		t.Fatalf("rotation engines = %v", got)
	}
	if got := cfg.GetFallbackEngines(); len(got) != 0 {
		t.Fatalf("explicit empty fallback must stay empty: %v", got)
	}
	cfg.FallbackEngines = nil
	if got := cfg.GetFallbackEngines(); len(got) != 2 {
		t.Fatalf("nil fallback should use defaults: %v", got)
	}
}

func TestMCPHubServerKeys(t *testing.T) {
	s := MCPHubServerConfig{APIKey: " one ", SKList: []string{" two ", "three"}}
	keys := s.EffectiveKeys()
	if len(keys) != 2 || keys[0] != " two " || keys[1] != "three" {
		t.Fatalf("keys = %#v", keys)
	}
	if got := (MCPHubServerConfig{APIKey: "one"}).EffectiveKeys(); len(got) != 1 || got[0] != "one" {
		t.Fatalf("single key = %#v", got)
	}
	if got := (MCPHubServerConfig{}).EffectiveKeys(); got != nil {
		t.Fatalf("empty keys = %#v", got)
	}
}
