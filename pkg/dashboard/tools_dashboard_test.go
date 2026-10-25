package dashboard

import (
	"encoding/json"
	"strings"
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
	conf.PDFParser.MinerUPageLimit = 450
	conf.Everything.URL = "http://127.0.0.1:9999"
	conf.Everything.Roots = []string{`D:\private-project`, `E:\private-documents`}

	out := buildDashboardOverview(conf, telemetry.Overview{}, testLoc())
	if len(out.ConfiguredTools) != 5 {
		t.Fatalf("configured tools = %d, want 5", len(out.ConfiguredTools))
	}
	names := make([]string, 0, len(out.ConfiguredTools))
	for _, tool := range out.ConfiguredTools {
		names = append(names, tool.Name)
		if !tool.Enabled {
			t.Fatalf("tool %s should be enabled in this config", tool.Name)
		}
		if tool.Registered || (tool.State != "pending" && tool.State != "unprobed") {
			t.Fatalf("configuration must not claim registration: %+v", tool)
		}
		if tool.Name == "pdf_parser" && (tool.PDF == nil || tool.PDF.MaxPages != conf.PDFParser.GetMaxPages() || tool.PDF.MinerUPageLimit != conf.PDFParser.GetMinerUPageLimit() || tool.PDF.PageProgress != "not_collected") {
			t.Fatalf("PDF settings/progress boundary missing: %+v", tool)
		}
		if tool.Name == "file_search" && (tool.File == nil || tool.File.AllowedRootCount != 2 || !tool.File.PathMetadataOnly || !tool.File.WhitelistEnforced) {
			t.Fatalf("file access boundary missing: %+v", tool)
		}
	}
	want := []string{"smartsearch", "academicsearch", "cleanfetch", "pdf_parser", "file_search"}
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

func TestCleanFetchViewKeepsReadsEnabled(t *testing.T) {
	tools := mcpToolCatalog(config.Config{}, testLoc())
	for _, tool := range tools {
		if tool.Name == "cleanfetch" && (!tool.Enabled || tool.FetchEnabled == nil || *tool.FetchEnabled) {
			t.Fatalf("saved reads and URL fetching must have distinct status: %+v", tool)
		}
	}
}

func TestFileToolViewReportsBoundaryWithoutSensitivePaths(t *testing.T) {
	conf := config.Config{}
	conf.Everything.URL = "http://private-endpoint:9999"
	conf.Everything.Username = "private-username"
	conf.Everything.Password = "private-password"
	before := mcpToolCatalog(conf, testLoc())
	conf.Everything.Roots = []string{`D:\private-root`, `E:\private-root`}
	after := mcpToolCatalog(conf, testLoc())
	for i := range after {
		if after[i].Name != before[i].Name || after[i].Enabled != before[i].Enabled || after[i].Registered != before[i].Registered || after[i].State != before[i].State {
			t.Fatalf("root summary changed tool registration: before=%+v after=%+v", before[i], after[i])
		}
	}
	file := before[4].File
	if file == nil || file.AllowedRootCount != 0 || file.WhitelistEnforced || !file.PathMetadataOnly {
		t.Fatalf("empty whitelist must report unrestricted directory scope: %+v", file)
	}
	raw, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-root", "private-endpoint", "private-username", "private-password"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("tool summary exposes sensitive configuration: %s", private)
		}
	}
}
