package dashboard

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func settingsFixture(t *testing.T) (config.Config, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	b := []byte("mode: hybrid\nnetwork: international\nbing:\n  enabled: true\nacademic:\n  enabled: true\nunknown:\n  preserved: yes\n")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c := *config.Default()
	c.Mode, c.Network = "hybrid", "international"
	c.Dashboard.StoragePath = filepath.Join(dir, "dashboard.db")
	c.Dashboard.Suspension.BanTimeOnFail = "5s"
	c.Dashboard.Suspension.MaxBanTimeOnFail = "24h"
	return c, path, b
}

func TestSettingsSaveConflictReadbackRestore(t *testing.T) {
	c, path, b := settingsFixture(t)
	req := SettingsRequest{Changes: map[string]any{"anysearch.enabled": false}, Revision: configRevision(b)}
	if out, err := applySettingsForTest(c, req, path, nil); err != nil || out.Applied {
		t.Fatalf("preview: %#v %v", out, err)
	}
	check, _ := os.ReadFile(path)
	if !bytes.Equal(b, check) {
		t.Fatal("preview wrote config")
	}
	req.Confirm = true
	cleared := 0
	out, err := applySettingsForTest(c, req, path, func() error { cleared++; return nil })
	if err != nil || !out.Applied || cleared != 1 {
		t.Fatalf("save: %#v %v", out, err)
	}
	check, _ = os.ReadFile(path)
	if !strings.Contains(string(check), "preserved: yes") {
		t.Fatal("non-target setting lost")
	}
	if _, err := applySettingsForTest(c, req, path, nil); !errors.Is(err, errSettingsConflict) {
		t.Fatalf("stale write: %v", err)
	}
	view := settingsFileViewForTest(c, path)
	if view["pending_changes"].(map[string]any)["anysearch.enabled"] != false {
		t.Fatal("missing pending state")
	}
	restore := SettingsRequest{RestoreBackup: out.Backup, Revision: out.Revision, Confirm: true}
	if _, err := applySettingsForTest(c, restore, path, nil); err != nil {
		t.Fatal(err)
	}
	check, _ = os.ReadFile(path)
	values, err := settingsFromDocument(c, check)
	if err != nil || values["anysearch.enabled"] != true {
		t.Fatalf("restore: %v %v", values, err)
	}
}

func TestSettingsLastSourceAndTrustBoundaries(t *testing.T) {
	c, path, b := settingsFixture(t)
	for _, changes := range []map[string]any{
		{"anysearch.enabled": false, "bing.enabled": false, "duckduckgo.enabled": false},
		{"academic.disable_arxiv": true, "academic.disable_crossref": true, "academic.disable_openalex": true, "academic.disable_pubmed": true, "academic.disable_europepmc": true, "academic.disable_dblp": true, "academic.disable_doaj": true, "academic.disable_semantic_scholar": true, "academic.disable_google_scholar": true},
		{"bing.enabled": "false"},
		{"dashboard.admin_password": "unsafe"},
		{"smartsearch.max_size": 1.5},
	} {
		if _, err := applySettingsForTest(c, SettingsRequest{Changes: changes, Revision: configRevision(b), Confirm: true}, path, nil); err == nil {
			t.Fatalf("accepted %v", changes)
		}
		check, _ := os.ReadFile(path)
		if !bytes.Equal(check, b) {
			t.Fatal("invalid request changed file")
		}
	}
	t.Setenv("APP_BING.ENABLED", "true")
	if _, err := applySettingsForTest(c, SettingsRequest{Changes: map[string]any{"bing.enabled": false}}, path, nil); err == nil {
		t.Fatal("environment locked field edited")
	}
	if _, err := applySettingsForTest(c, SettingsRequest{RestoreBackup: "../config-stolen.yaml"}, path, nil); err == nil {
		t.Fatal("backup traversal accepted")
	}
}

func TestSettingsCacheFailureRollsBack(t *testing.T) {
	c, path, b := settingsFixture(t)
	_, err := applySettingsForTest(c, SettingsRequest{Changes: map[string]any{"anysearch.enabled": false}, Revision: configRevision(b), Confirm: true}, path, func() error { return errors.New("cache unavailable") })
	if err == nil {
		t.Fatal("cache failure reported as success")
	}
	check, _ := os.ReadFile(path)
	if !bytes.Equal(check, b) {
		t.Fatal("rollback failed")
	}
}

func TestDoubaoStartupSettingsSaveAndRestoreOmittedDefaults(t *testing.T) {
	c, path, b := settingsFixture(t)
	if err := validateSetting("doubao.icp_host_only", "true"); err == nil {
		t.Fatal("ICP filter must require a boolean")
	}
	cleared := 0
	changes := map[string]any{"doubao.icp_host_only": true, "doubao.version": "custom"}
	out, err := applySettingsForTest(c, SettingsRequest{Changes: changes, Revision: configRevision(b), Confirm: true}, path, func() error { cleared++; return nil })
	if err != nil || !out.Applied || cleared != 1 {
		t.Fatalf("save: %#v %v; cache clears %d", out, err, cleared)
	}
	saved, err := configWithSettings(c, map[string]any{"doubao.icp_host_only": true, "doubao.version": "custom"})
	if err != nil || !saved.Doubao.ICPHostOnly || saved.Doubao.GetVersion() != "custom" {
		t.Fatalf("candidate: %#v %v", saved.Doubao, err)
	}
	if _, err := applySettingsForTest(saved, SettingsRequest{RestoreBackup: out.Backup, Revision: out.Revision, Confirm: true}, path, func() error { cleared++; return nil }); err != nil {
		t.Fatal(err)
	}
	check, _ := os.ReadFile(path)
	values, err := settingsFromDocument(saved, check)
	if err != nil || values["doubao.icp_host_only"] != false || values["doubao.version"] != "global" || cleared != 2 {
		t.Fatalf("restore defaults: %#v %v; cache clears %d", values, err, cleared)
	}
	t.Setenv("APP_DOUBAO.ICP_HOST_ONLY", "true")
	if _, err := applySettingsForTest(saved, SettingsRequest{Changes: map[string]any{"doubao.icp_host_only": false}}, path, nil); err == nil {
		t.Fatal("environment-controlled ICP filter edited")
	}
}

func TestSettingsSourceEditPreservesUnsetDashboardDefaults(t *testing.T) {
	c, path, b := settingsFixture(t)
	c.Dashboard.Suspension = config.SuspensionConfig{}
	out, err := applySettingsForTest(c, SettingsRequest{Changes: map[string]any{"anysearch.enabled": false}, Revision: configRevision(b), Confirm: true}, path, nil)
	if err != nil || !out.Applied {
		t.Fatalf("source edit with omitted dashboard defaults: %#v %v", out, err)
	}
	check, _ := os.ReadFile(path)
	if strings.Contains(string(check), "suspension") {
		t.Fatal("source edit wrote unrelated dashboard defaults")
	}
}

func TestLastSourceRequiresResolvedProxyButDoesNotProbeIt(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	c := *config.Default()
	c.Mode, c.Network = "engine", "international"
	c.Bing.Enabled, c.DuckDuckGo.Enabled, c.Academic.Enabled = false, true, false
	if c.Proxy.GetProxyEndpoint() != "" {
		t.Skip("a system proxy is configured outside the test environment")
	}
	if err := validateSourceCombinationForTest(c); err == nil {
		t.Fatal("unresolved proxy counted as the last usable source")
	}
	c.Proxy = config.ProxyConfig{Enabled: true, Endpoint: "http://127.0.0.1:1"}
	if err := validateSourceCombinationForTest(c); err != nil {
		t.Fatalf("candidate validation must not probe upstream availability: %v", err)
	}
}

func TestWriteNetworksDoNotTrustForwardedHeaders(t *testing.T) {
	c := config.Config{}
	c.Dashboard.AdminPassword = "pw"
	c.Dashboard.AllowedNetworks = []string{"192.168.0.0/16"}
	c.Dashboard.WriteAllowedNetworks = []string{"172.18.0.1/32"}
	_, mux := newWriteTestHandler(t, c)
	for _, tc := range []struct {
		remote, pass string
		want         int
	}{
		{"172.18.0.1:123", "pw", http.StatusAccepted},
		{"172.18.0.1:123", "bad", http.StatusForbidden},
		{"172.18.0.2:123", "pw", http.StatusForbidden},
		{"192.168.1.2:123", "pw", http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodPost, "/__admin/api/restart", strings.NewReader(`{"confirm":true}`))
		r.RemoteAddr = tc.remote
		r.Host = "127.0.0.1"
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		r.Header.Set("X-Real-IP", "172.18.0.1")
		r.Header.Set("X-Admin-Password", tc.pass)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.remote, tc.pass, w.Code)
		}
	}
	c.Dashboard.WriteAllowedNetworks = []string{"invalid"}
	_, mux = newWriteTestHandler(t, c)
	if doWrite(t, mux, http.MethodPost, "/__admin/api/restart", `{"confirm":true}`, "172.18.0.1:123", "pw").Code != http.StatusForbidden {
		t.Fatal("invalid CIDR did not fail closed")
	}
}
