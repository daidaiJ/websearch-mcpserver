package dashboard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func TestExpandedSettingsValidateThroughPreview(t *testing.T) {
	c, path, _ := settingsFixture(t)
	valid := map[string]any{
		"apipool.strategy":                "weighted",
		"apipool.engines":                 []any{"mcphub", "anysearch"},
		"apipool.weights":                 map[string]any{"mcphub": float64(4), "yuanbao": float64(0)},
		"black_list_host":                 []any{"example.org", "sub.example.net"},
		"llm.base_url":                    "https://api.example.org/v1",
		"smartsearch.engines.bing.weight": float64(1.5),
		"everything.max_results":          float64(20),
	}
	if _, err := applySettingsForTest(c, SettingsRequest{Changes: valid}, path, nil); err != nil {
		t.Fatalf("preview of supported settings failed: %v", err)
	}

	for name, value := range map[string]any{
		"apipool.engines":                    []any{"anysearch", "anysearch"},
		"apipool.weights":                    map[string]any{"bing": float64(5)},
		"black_list_host":                    []any{"https://example.org/path"},
		"llm.base_url":                       "https://api.example.org/?api_key=dummy-credential",
		"proxy.endpoint":                     "http://user:dummy-credential@127.0.0.1:7897",
		"rate_limit.per_sec":                 1.5,
		"everything.max_results":             float64(21),
		"pdf_parser.mineru_page_batch_size":  float64(4),
		"smartsearch.engines.unknown.weight": float64(1),
		"dashboard.admin_username":           "dummy-credential",
		"everything.roots":                   []any{"D:/private"},
	} {
		if _, err := applySettingsForTest(c, SettingsRequest{Changes: map[string]any{name: value}}, path, nil); err == nil {
			t.Errorf("settings preview accepted invalid field %s", name)
		}
	}
}

func TestSettingsHideSensitiveEndpointsAndLockEnvironment(t *testing.T) {
	c, path, original := settingsFixture(t)
	main := append(original, []byte("proxy:\n  endpoint: http://name:dummy-url-secret@127.0.0.1:7897\n")...)
	if err := os.WriteFile(path, main, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LLM_BASE_URL", "https://api.example.org/v1?token=dummy-env-secret")
	t.Setenv("WEBCRAWLER_LANGUAGE", "en")
	out := settingsFileViewForTest(c, path)
	view, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"dummy-url-secret", "dummy-env-secret"} {
		if strings.Contains(string(view), secret) {
			t.Fatalf("settings response exposed %s", secret)
		}
	}
	locked, _ := out["locked_fields"].(map[string]string)
	for _, name := range []string{"proxy.endpoint", "llm.base_url", "language"} {
		if locked[name] == "" {
			t.Errorf("missing lock on %s", name)
		}
	}
	for name, value := range map[string]any{
		"proxy.endpoint": "http://127.0.0.1:7897",
		"llm.base_url":   "https://api.example.org/v1",
		"language":       "zh",
	} {
		if _, err := applySettingsForTest(c, SettingsRequest{Changes: map[string]any{name: value}}, path, nil); err == nil {
			t.Errorf("preview accepted a change to locked field %s", name)
		}
	}
	req := SettingsRequest{Changes: map[string]any{"proxy.enabled": false}, Revision: out["revision"].(string), Confirm: true}
	if _, err := applySettingsForTest(c, req, path, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(saved), "dummy-url-secret") {
		t.Fatalf("unrelated save lost the original proxy URL: %v", err)
	}
}

func TestSearchSettingsUseManagedMCPHubAndClearPreviousResults(t *testing.T) {
	c, path, b := settingsFixture(t)
	req := SettingsRequest{Changes: map[string]any{
		"mode":                     "mcphub",
		"bing.enabled":             false,
		"duckduckgo.enabled":       false,
		"anysearch.enabled":        false,
		"mcp_hub.rotation_engines": []any{},
		"mcp_hub.fallback_engines": []any{},
		"mcp_hub.strategy":         "round-robin",
	}, Revision: configRevision(b), Confirm: true}
	clears := 0
	out, err := applySettingsForTest(c, req, path, func() error { clears++; return nil })
	if err != nil || !out.Applied || clears != 1 {
		t.Fatalf("mcphub mode save: applied=%v, clears=%d, error=%v", out.Applied, clears, err)
	}
	next, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := decodeSettingsDocuments(c, next, nil)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.GetMode() != config.ModeMCPHub || len(restarted.MCPHub.GetFallbackEngines()) != 0 {
		t.Fatal("saved mode or explicit empty fallback changed during startup decoding")
	}
	sources := SourceCatalog(restarted, testLoc())
	for _, name := range []string{"parallel", "anysearch", "exa", "firecrawl"} {
		source := sourceByID(t, sources, "mcp_hub_"+name)
		if source.Category != "mcphub" || !source.Active || source.ControlPath != "" {
			t.Errorf("unexpected hosted source %s: category=%s active=%v path=%s", name, source.Category, source.Active, source.ControlPath)
		}
	}
	if sourceByID(t, sources, "anysearch").Active {
		t.Fatal("hosted AnySearch must have a distinct switch and identity")
	}
}

func TestAPIPoolMembershipCanEnableKeenableWithoutWritingItsSwitch(t *testing.T) {
	c, path, b := settingsFixture(t)
	expectImplicit := func() {
		t.Helper()
		values := settingsFileViewForTest(c, path)["saved_values"].(map[string]any)
		if value, ok := values["keenable.enabled"]; !ok || value != nil {
			t.Fatalf("implicit Keenable switch = %v, want null", value)
		}
	}
	expectImplicit()
	req := SettingsRequest{Changes: map[string]any{
		"mode":            "apipool",
		"apipool.engines": []any{"keenable"},
	}, Revision: configRevision(b), Confirm: true}
	out, err := applySettingsForTest(c, req, path, nil)
	if err != nil || !out.Applied {
		t.Fatalf("changing API pool membership failed: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := decodeSettingsDocuments(c, saved, nil)
	if err != nil || restarted.Keenable.Enabled != nil {
		t.Fatalf("pool membership wrote an unrelated source switch: %v", err)
	}
	expectImplicit()
	if source := sourceByID(t, SourceCatalog(restarted, testLoc()), "keenable"); !source.Active {
		t.Fatal("keyless Keenable does not participate in the API pool")
	}
}
