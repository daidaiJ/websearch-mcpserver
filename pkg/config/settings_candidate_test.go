package config

import (
	"reflect"
	"testing"
)

func TestCandidateConfigMatchesStartupDefaultsAndOverlayOverrides(t *testing.T) {
	main := []byte("proxy:\n  enabled: false\nmcp_hub:\n  fallback_engines: []\ndashboard:\n  quotas:\n    limits:\n      tavily: 42\n  brand:\n    title: Customized\n    accent: '#123456'\n")
	overlay := []byte("dashboard:\n  quotas:\n    limits: {}\n  brand:\n    title: ''\n    accent: ''\n    footer: false\n")
	candidate, err := DecodeSettingsCandidate(main, overlay, SettingsFilePaths{})
	if err != nil {
		t.Fatal(err)
	}
	if enabled := candidate.Proxy.EnabledSetting(); enabled == nil || *enabled {
		t.Fatalf("explicit proxy disable decoded as %v", enabled)
	}
	if candidate.SmartSearch.MMR.Lambda != 0.7 || candidate.Academic.Threshold != 0.02 || !candidate.PDFParser.MinerURemotePDF {
		t.Fatal("candidate skipped startup defaults")
	}
	if len(candidate.MCPHub.GetFallbackEngines()) != 0 || candidate.MCPHub.GetTimeout().Seconds() != 20 {
		t.Fatal("candidate lost an explicit empty engine list or startup MCPHub timeout")
	}
	if candidate.Dashboard.Quotas.GetLimit("tavily") != 42 || candidate.Dashboard.Brand.GetTitle() != "Customized" ||
		candidate.Dashboard.Brand.GetAccent() != "#123456" || candidate.Dashboard.Brand.GetFooter() {
		t.Fatalf("candidate did not inherit legacy empty overlay values or retain explicit false: %+v", candidate.Dashboard)
	}
	fallback, err := DecodeSettingsCandidate(main, nil, SettingsFilePaths{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fallback.Dashboard.Quotas.Limits, map[string]int{"tavily": 42}) || fallback.Dashboard.Brand.Title != "Customized" ||
		fallback.Dashboard.Brand.GetAccent() != "#123456" || !fallback.Dashboard.Brand.GetFooter() {
		t.Fatal("removing the overlay failed to restore main dashboard values")
	}
}
