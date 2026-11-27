package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"websearch/pkg/config"
)

func TestSettingsOverlayHTTP(t *testing.T) {
	c, path, main := settingsFixture(t)
	t.Setenv("WEBSEARCH_CONFIG", path)
	main = append(main, []byte(fmt.Sprintf("dashboard:\n  storage_path: %q\n", filepath.ToSlash(c.Dashboard.StoragePath)))...)
	if err := os.WriteFile(path, main, 0o600); err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(filepath.Dir(path), "custom-dashboard.yaml")
	t.Setenv("WEBSEARCH_DASHBOARD_CONFIG", overlayPath)
	for _, name := range []string{"BAIDU_SK", "TAVILY_SK", "EXA_API_KEY", "ANYSEARCH_API_KEY", "DOUBAO_SEARCH_API_KEY", "TENCENTCLOUD_WSA_APIKEY", "MINERU_TOKEN", "UNPAYWALL_EMAIL", "OPENALEX_API_KEY", "GITHUB_TOKEN", "LLM_API_KEY", "LLM_BASE_URL"} {
		t.Setenv(name, "")
	}
	overlay := []byte("# local dashboard\ndashboard:\n  admin_password: http-test-password # keep password comment\n  allowed_networks: ['192.0.2.0/24']\n  write_allowed_networks: ['127.0.0.1/32']\n  suspension:\n    ban_time_on_fail: 10s # keep duration comment\n")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, mux := newWriteTestHandler(t, *loaded)
	call := func(method string, request any, status int, response any) {
		t.Helper()
		var body []byte
		if request != nil {
			body, err = json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
		}
		res := doWrite(t, mux, method, "/__admin/api/settings", string(body), "", "http-test-password")
		if res.Code != status {
			t.Fatalf("settings %s status = %d, want %d: %s", method, res.Code, status, res.Body)
		}
		if strings.Contains(res.Body.String(), "http-test-password") || strings.Contains(res.Body.String(), "127.0.0.1/32") || strings.Contains(res.Body.String(), "192.0.2.0/24") || strings.Contains(res.Body.String(), "must-not-echo") {
			t.Fatal("settings disclosed an overlay security value")
		}
		if response != nil {
			if err := json.Unmarshal(res.Body.Bytes(), response); err != nil {
				t.Fatal(err)
			}
		}
	}
	files := func() ([]byte, []byte) {
		t.Helper()
		m, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		d, err := os.ReadFile(overlayPath)
		if err != nil {
			t.Fatal(err)
		}
		return m, d
	}

	var view struct {
		Revision   string            `json:"revision"`
		Saved      map[string]any    `json:"saved_values"`
		Locked     map[string]string `json:"locked_fields"`
		FieldFiles map[string]string `json:"field_files"`
	}
	const field = "dashboard.suspension.ban_time_on_fail"
	call(http.MethodGet, nil, http.StatusOK, &view)
	if view.Revision == "" || view.Saved[field] != "10s" || view.Locked[field] != "" || view.FieldFiles[field] != "dashboard.yaml" {
		t.Fatalf("overlay setting unavailable: saved=%v lock=%q file=%q", view.Saved[field], view.Locked[field], view.FieldFiles[field])
	}
	for forbidden, value := range map[string]any{
		"dashboard.admin_password":         "must-not-echo",
		"dashboard.admin_password_sha256":  "must-not-echo",
		"dashboard.admin_username":         "must-not-echo",
		"dashboard.allowed_networks":       []string{"0.0.0.0/0"},
		"dashboard.write_allowed_networks": []string{"0.0.0.0/0"},
		"dashboard.storage_path":           "changed.db",
		"dashboard.config_path":            "different.yaml",
		"dashboard.secrets_path":           "different.json",
		"dashboard.enabled":                false,
		"host":                             "0.0.0.0",
		"port":                             9945,
		"everything.url":                   "http://127.0.0.1:1",
		"everything.roots":                 []string{"D:/private"},
	} {
		call(http.MethodPost, SettingsRequest{Revision: view.Revision, Changes: map[string]any{forbidden: value}, Confirm: true}, http.StatusBadRequest, nil)
	}
	req := SettingsRequest{Revision: view.Revision, Changes: map[string]any{field: "15s"}}
	var preview SettingsResult
	call(http.MethodPost, req, http.StatusOK, &preview)
	if preview.Applied {
		t.Fatal("preview applied changes")
	}
	if m, d := files(); !bytes.Equal(m, main) || !bytes.Equal(d, overlay) {
		t.Fatal("preview changed files")
	}

	req.Confirm = true
	var out SettingsResult
	call(http.MethodPost, req, http.StatusOK, &out)
	if !out.Applied || out.Revision == view.Revision || out.Backup == "" {
		t.Fatal("save did not advance the revision and record a backup")
	}
	if m, d := files(); !bytes.Equal(m, main) || !strings.Contains(string(d), "ban_time_on_fail: 15s # keep duration comment") || !strings.Contains(string(d), "http-test-password # keep password comment") || !strings.Contains(string(d), "# local dashboard") {
		t.Fatal("save changed the main file or failed to preserve overlay comments")
	}
	call(http.MethodPost, req, http.StatusConflict, nil)

	call(http.MethodPost, SettingsRequest{Revision: out.Revision, RestoreBackup: out.Backup, Confirm: true}, http.StatusOK, nil)
	if m, d := files(); !bytes.Equal(m, main) || !strings.Contains(string(d), "ban_time_on_fail: 10s # keep duration comment") {
		t.Fatal("overlay restore wrote outside its owning file or failed to restore the setting")
	}
}

func TestMixedSettingsSaveRollbackAndRestore(t *testing.T) {
	c, path, main := settingsFixture(t)
	overlayPath := filepath.Join(filepath.Dir(path), "dashboard.yaml")
	overlay := []byte("dashboard:\n  retention_days: 28\n  brand:\n    title: Existing\n")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	assertFiles := func(wantMain, wantOverlay []byte) {
		t.Helper()
		mainCheck, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(mainCheck, wantMain) {
			t.Fatal("main configuration differs")
		}
		overlayCheck, err := os.ReadFile(overlayPath)
		if err != nil || !bytes.Equal(overlayCheck, wantOverlay) {
			t.Fatal("dashboard configuration differs")
		}
	}
	apply := func(req SettingsRequest, clearCache func() error) (SettingsResult, error) {
		return applySettingsFile(c, req, path, overlayPath, clearCache, testLoc())
	}
	view := settingsFileView(c, path, overlayPath, testLoc())
	const field = "dashboard.suspension.ban_time_on_fail"
	req := SettingsRequest{
		Revision: view["revision"].(string),
		Confirm:  true,
		Changes:  map[string]any{"anysearch.enabled": false, field: "11s"},
	}
	if req.Revision == "" {
		t.Fatal("configuration view has no revision")
	}

	// Simulate an external overlay edit between preview and confirmation.
	withComment := append(bytes.Clone(overlay), []byte("# external edit\n")...)
	if err := os.WriteFile(overlayPath, withComment, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(req, nil); !errors.Is(err, errSettingsConflict) {
		t.Fatalf("stale mixed request: %v", err)
	}
	assertFiles(main, withComment)
	req.Revision = settingsFileView(c, path, overlayPath, testLoc())["revision"].(string)

	cleared := false
	if _, err := apply(req, func() error {
		cleared = true
		mainCheck, mainErr := os.ReadFile(path)
		overlayCheck, overlayErr := os.ReadFile(overlayPath)
		if mainErr != nil || overlayErr != nil || !strings.Contains(string(mainCheck), "anysearch:") || !strings.Contains(string(overlayCheck), "ban_time_on_fail: 11s") {
			t.Error("cache invalidated before both edits were committed")
		}
		return errors.New("cache unavailable")
	}); err == nil || !cleared {
		t.Fatalf("mixed write did not surface a cache clear failure: %v", err)
	}
	assertFiles(main, withComment)

	out, err := apply(req, nil)
	if err != nil || !out.Applied || !strings.HasPrefix(out.Backup, "settings-") {
		t.Fatalf("mixed save: %#v %v", out, err)
	}
	restored := SettingsRequest{Revision: out.Revision, RestoreBackup: out.Backup, Confirm: true}
	damagedBackup := filepath.Join(overlayBackupDirectory(c), out.Backup)
	backup, err := os.ReadFile(damagedBackup)
	if err != nil || !bytes.Equal(backup, withComment) {
		t.Fatal("dashboard preimage missing or altered")
	}
	if err := os.Remove(damagedBackup); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(restored, nil); err == nil {
		t.Fatal("restored one half of a mixed backup")
	}
	if err := os.WriteFile(damagedBackup, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	restoredOut, err := apply(restored, nil)
	if err != nil || !restoredOut.Applied || restoredOut.Changes["anysearch.enabled"] != true {
		t.Fatalf("mixed restore: %#v %v", restoredOut, err)
	}
	mainCheck, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(mainCheck), "enabled: false") {
		t.Fatal("mixed restore failed to remove newly added source switch")
	}
	overlayCheck, err := os.ReadFile(overlayPath)
	if err != nil || strings.Contains(string(overlayCheck), "ban_time_on_fail") || !strings.Contains(string(overlayCheck), "# external edit") {
		t.Fatal("mixed restore failed to remove the newly added dashboard leaf or lost a comment")
	}
}

func TestSettingsRestoreLegacyDefaultsAndLocks(t *testing.T) {
	c, path, main := settingsFixture(t)
	overlayPath := filepath.Join(filepath.Dir(path), "dashboard.yaml")
	main = append(main, []byte("proxy:\n  enabled: null\nllm:\n  model_id: old-model\ndashboard:\n  retention_days: 42\n  quotas:\n    reset_day: 9\n")...)
	overlay := []byte("dashboard:\n  retention_days: 0\n  quotas:\n    reset_day: 0\n  admin_password: old-password\n")
	if err := os.WriteFile(path, main, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	apply := func(req SettingsRequest) (SettingsResult, error) {
		return applySettingsFile(c, req, path, overlayPath, nil, testLoc())
	}
	t.Setenv("APP_LLM.MODEL_ID", "locked-model")
	for _, field := range []string{"dashboard.retention_days", "dashboard.quotas.reset_day"} {
		if _, err := apply(SettingsRequest{Changes: map[string]any{field: float64(0)}}); err == nil {
			t.Fatalf("new edits accepted out-of-range zero for %s", field)
		}
	}
	view := settingsFileView(c, path, overlayPath, testLoc())
	out, err := apply(SettingsRequest{
		Revision: view["revision"].(string), Confirm: true,
		Changes: map[string]any{"dashboard.retention_days": float64(14), "dashboard.quotas.reset_day": float64(3), "proxy.enabled": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	activeMain, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(activeMain, []byte("old-model"), []byte("manual-model")), 0o600); err != nil {
		t.Fatal(err)
	}
	activeOverlay, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, bytes.ReplaceAll(activeOverlay, []byte("old-password"), []byte("manual-password")), 0o600); err != nil {
		t.Fatal(err)
	}
	view = settingsFileView(c, path, overlayPath, testLoc())
	restored, err := apply(SettingsRequest{Revision: view["revision"].(string), RestoreBackup: out.Backup, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]any{"dashboard.retention_days": float64(42), "dashboard.quotas.reset_day": float64(9), "proxy.enabled": nil} {
		if actual, ok := restored.Changes[field]; !ok || canonicalValue(actual) != want {
			t.Errorf("restored %s = %v, want %v", field, actual, want)
		}
	}
	mainCheck, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(mainCheck, []byte("enabled: null")) || !bytes.Contains(mainCheck, []byte("manual-model")) {
		t.Fatal("restore lost the nullable setting or rewrote an environment-locked field")
	}
	dashboardCheck, err := os.ReadFile(overlayPath)
	if err != nil || !bytes.Contains(dashboardCheck, []byte("manual-password")) {
		t.Fatal("restore rewrote a protected setting")
	}
}

func TestSettingsRequireRestartAfterConfigurationPathsChange(t *testing.T) {
	for _, key := range []string{"config_path", "secrets_path"} {
		t.Run(key, func(t *testing.T) {
			_, path, main := settingsFixture(t)
			t.Setenv("WEBSEARCH_CONFIG", path)
			t.Setenv("WEBSEARCH_DASHBOARD_CONFIG", "")
			dir := filepath.Dir(path)
			a, b := "private-dashboard-a.yaml", "private-dashboard-b.yaml"
			sa, sb := filepath.Join(dir, "private-secrets-a.json"), filepath.Join(dir, "private-secrets-b.json")
			content := []byte("dashboard:\n  admin_password: path-test-password\n  retention_days: 23\n")
			for _, file := range []string{a, b} {
				if err := os.WriteFile(filepath.Join(dir, file), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range []string{sa, sb} {
				if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			initial := append(main, []byte(fmt.Sprintf("dashboard:\n  config_path: %s\n  secrets_path: %q\n", a, filepath.ToSlash(sa)))...)
			if err := os.WriteFile(path, initial, 0o600); err != nil {
				t.Fatal(err)
			}
			conf, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			_, mux := newWriteTestHandler(t, *conf)
			api := "/__admin/api/settings"
			var view struct {
				Revision    string                 `json:"revision"`
				ConfigError string                 `json:"config_error"`
				WriteAccess struct{ Allowed bool } `json:"write_access"`
			}
			initialView := doWrite(t, mux, http.MethodGet, api, "", "", "")
			if err := json.Unmarshal(initialView.Body.Bytes(), &view); err != nil || !view.WriteAccess.Allowed || view.Revision == "" {
				t.Fatalf("settings unavailable before the path changes: %v", err)
			}
			oldRev := view.Revision
			changed := bytes.ReplaceAll(initial, []byte(a), []byte(b))
			if key == "secrets_path" {
				changed = bytes.ReplaceAll(initial, []byte(filepath.ToSlash(sa)), []byte(filepath.ToSlash(sb)))
			}
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			view.Revision = ""
			resp := doWrite(t, mux, http.MethodGet, api, "", "", "")
			if err := json.Unmarshal(resp.Body.Bytes(), &view); err != nil || view.WriteAccess.Allowed || view.Revision != "" || view.ConfigError == "" {
				t.Fatalf("settings still writable after %s changed: %v", key, err)
			}
			for _, confirm := range []bool{false, true} {
				body := fmt.Sprintf("{\"changes\":{\"dashboard.retention_days\":11},\"revision\":%q,\"confirm\":%t}", oldRev, confirm)
				res := doWrite(t, mux, http.MethodPost, api, body, "", "path-test-password")
				if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), view.ConfigError) {
					t.Errorf("path change did not return a restart conflict: %d %s", res.Code, res.Body)
				}
				if strings.Contains(res.Body.String(), b) || strings.Contains(res.Body.String(), filepath.Base(sb)) {
					t.Error("path conflict disclosed a private file path")
				}
			}
			for _, file := range []string{a, b} {
				current, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil || !bytes.Equal(current, content) {
					t.Errorf("path change modified an overlay file: %v", err)
				}
			}
		})
	}
}

func TestSettingsOverlayInheritanceAndEnvironmentLocks(t *testing.T) {
	c, path, main := settingsFixture(t)
	overlayPath := filepath.Join(filepath.Dir(path), "dashboard.yaml")
	main = append(main, []byte("dashboard:\n  retention_days: 42\n  quotas:\n    limits: {tavily: 550}\n  brand:\n    title: Main Branding\n    accent: '#123456'\n")...)
	if err := os.WriteFile(path, main, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_DASHBOARD.RETENTION_DAYS", "61")
	for _, tc := range []struct {
		name, retention string
		want            float64
		locked          bool
	}{
		{"overlay-active", "20", 20, false},
		{"overlay-inherits", "0", 61, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overlay := []byte("dashboard:\n  retention_days: " + tc.retention + "\n  quotas:\n    limits: {}\n  brand:\n    title: ''\n    accent: ''\n")
			if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
				t.Fatal(err)
			}
			view := settingsFileView(c, path, overlayPath, testLoc())
			values := view["saved_values"].(map[string]any)
			limits, ok := canonicalValue(values["dashboard.quotas.limits"]).(map[string]any)
			if !ok || limits["tavily"] != float64(550) || values["dashboard.brand.title"] != "Main Branding" || values["dashboard.brand.accent"] != "#123456" {
				t.Error("empty legacy overlay fields masked main quota or branding")
			}
			const key = "dashboard.retention_days"
			locked := view["locked_fields"].(map[string]string)[key] != ""
			if effective := canonicalValue(values[key]); effective != tc.want || locked != tc.locked {
				t.Errorf("retention = %v; locked = %v; want %v, %v", effective, locked, tc.want, tc.locked)
			}
			preview, err := applySettingsFile(c, SettingsRequest{Revision: view["revision"].(string), Changes: map[string]any{key: float64(24)}}, path, overlayPath, nil, testLoc())
			if tc.locked {
				if err == nil {
					t.Fatal("preview accepted a setting overridden by the environment")
				}
			} else if err != nil || canonicalValue(preview.Changes[key]) != float64(24) {
				t.Fatalf("preview could not edit the effective overlay value: %v", err)
			}
		})
	}

	// A user clears previously explicit overlay values to inherit the primary file.
	overlay := []byte("dashboard:\n  quotas:\n    limits: {tavily: 500}\n  brand:\n    title: Overlay Branding\n    accent: '#654321'\n")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	view := settingsFileView(c, path, overlayPath, testLoc())
	preview, err := applySettingsFile(c, SettingsRequest{
		Revision: view["revision"].(string),
		Changes:  map[string]any{"dashboard.quotas.limits": map[string]any{}, "dashboard.brand.title": "", "dashboard.brand.accent": ""},
	}, path, overlayPath, nil, testLoc())
	if err != nil {
		t.Fatal(err)
	}
	limits, ok := canonicalValue(preview.Changes["dashboard.quotas.limits"]).(map[string]any)
	if !ok || limits["tavily"] != float64(550) || preview.Changes["dashboard.brand.title"] != "Main Branding" || preview.Changes["dashboard.brand.accent"] != "#123456" {
		t.Fatal("clear preview did not return the inherited effective values")
	}
}
