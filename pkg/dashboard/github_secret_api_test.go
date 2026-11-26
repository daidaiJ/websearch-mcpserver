package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func TestGitHubTokenDashboardRoundTrip(t *testing.T) {
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Setenv("WEBSEARCH_DASHBOARD_CONFIG", "")
	t.Setenv("GITHUB_TOKEN", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const existingToken = "ghp_config-fixture"
	yaml := "dashboard:\n  storage_path: " + filepath.ToSlash(filepath.Join(dir, "dashboard.db")) +
		"\ngithub:\n  token_list: [" + existingToken + "]\n"
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	load := func() config.Config {
		t.Helper()
		conf, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		conf.Dashboard.AdminPassword = "test-password"
		return *conf
	}
	newMux := func(conf config.Config) *http.ServeMux {
		mux := http.NewServeMux()
		New(conf, nil, nil, nil).Register(mux, func(next http.HandlerFunc) http.HandlerFunc { return next })
		return mux
	}
	post := func(mux *http.ServeMux, value string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/__admin/api/secrets", strings.NewReader(`{"name":"GITHUB_TOKEN","value":"`+value+`","confirm":true}`))
		req.RemoteAddr = "127.0.0.1:30100"
		req.Header.Set("X-Admin-Password", "test-password")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || (value != "" && strings.Contains(rec.Body.String(), value)) {
			t.Fatalf("unexpected secret response status %d", rec.Code)
		}
	}
	const token = "ghp_dashboard-test-token"
	conf := load()
	post(newMux(conf), token)
	persisted, err := os.ReadFile(conf.GetDashboardSecretsPath())
	if err != nil || !bytes.Contains(persisted, []byte(token)) {
		t.Fatalf("overlay did not persist the token: %v", err)
	}
	conf = load()
	if keys := conf.GitHub.EffectiveTokenList(); len(keys) != 2 || keys[0] != token || keys[1] != existingToken {
		t.Fatal("reloaded configuration did not apply the override")
	}
	mux := newMux(conf)
	settings := getJSON(t, mux, "/__admin/api/settings")
	if !bytes.Contains(settings, []byte(`"GITHUB_TOKEN":true`)) || bytes.Contains(settings, []byte(token)) || bytes.Contains(settings, []byte(existingToken)) {
		t.Fatal("settings did not expose redacted token status")
	}
	post(mux, "")
	if keys := load().GitHub.EffectiveTokenList(); len(keys) != 1 || keys[0] != existingToken {
		t.Fatal("clearing did not restore the pre-existing token pool")
	}
	persisted, err = os.ReadFile(conf.GetDashboardSecretsPath())
	if err != nil || bytes.Contains(persisted, []byte("GITHUB_TOKEN")) {
		t.Fatalf("clearing did not remove the persisted override: %v", err)
	}
}
