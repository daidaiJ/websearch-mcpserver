package dashboard

import (
	"encoding/json"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

func TestOverviewListsAllMCPToolsWithoutObservedCall(t *testing.T) {
	conf := config.Config{}
	conf.Bing.Enabled = true
	conf.Academic.Enabled = true
	conf.CleanFetch.Enabled = true
	conf.PDFParser.Enabled = true

	out := buildDashboardOverview(conf, telemetry.Overview{})
	if len(out.ConfiguredTools) != 4 {
		t.Fatalf("configured tools = %d, want 4", len(out.ConfiguredTools))
	}
	names := make([]string, 0, len(out.ConfiguredTools))
	for _, tool := range out.ConfiguredTools {
		names = append(names, tool.Name)
		if !tool.Enabled {
			t.Fatalf("tool %s should be enabled in this config", tool.Name)
		}
	}
	want := []string{"smartsearch", "academicsearch", "cleanfetch", "pdf_parser"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("tool[%d] = %s, want %s", i, names[i], name)
		}
	}

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["configured_tools"]; !ok {
		t.Fatal("overview JSON missing configured_tools")
	}
}
