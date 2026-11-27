package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"websearch/pkg/config"
)

type credentialAPISlot struct {
	ID     string
	Source string
	Label  string
}

type credentialAPIStatus struct {
	Count    int
	Multi    bool
	ReadOnly bool `json:"read_only"`
	Sources  []string
	Slots    []credentialAPISlot
}

type credentialAPIView struct {
	Credentials    map[string]credentialAPIStatus
	Pending        map[string]credentialAPIStatus
	PendingRestart bool `json:"pending_restart"`
	Revision       string
}

var credentialTestNames = []string{
	"BAIDU_SK", "TAVILY_SK", "EXA_API_KEY", "ANYSEARCH_API_KEY", "DOUBAO_SEARCH_API_KEY",
	"TENCENTCLOUD_WSA_APIKEY", "TINYFISH_API_KEY", "KEENABLE_API_KEY", "GITHUB_TOKEN", "JINA_API_KEY",
	"MINERU_TOKEN", "LLM_API_KEY", "SEMANTIC_SCHOLAR_API_KEY", "OPENALEX_API_KEY", "UNPAYWALL_EMAIL",
}

func credentialFixture(t *testing.T, yaml string) (func() *config.Config, string) {
	t.Helper()
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Setenv("WEBSEARCH_DASHBOARD_CONFIG", "")
	for _, name := range credentialTestNames {
		t.Setenv(name, "")
	}
	t.Setenv("WEBSEARCH_TOKEN", "")
	t.Setenv("ASK_ECHO_SEARCH_INFINITY_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml+"\ndashboard:\n  storage_path: "+filepath.ToSlash(filepath.Join(dir, "dashboard.db"))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	load := func() *config.Config {
		t.Helper()
		conf, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		conf.Dashboard.AdminPassword = "test-password"
		return conf
	}
	return load, filepath.Join(dir, "dashboard-secrets.json")
}

func credentialMux(conf *config.Config) *http.ServeMux {
	mux := http.NewServeMux()
	New(*conf, nil, nil, nil).Register(mux, func(next http.HandlerFunc) http.HandlerFunc { return next })
	return mux
}

func callCredentialAPI(mux *http.ServeMux, operation map[string]any) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/__admin/api/secrets", nil)
	if operation != nil {
		body, _ := json.Marshal(operation)
		request = httptest.NewRequest(http.MethodPost, "/__admin/api/secrets", bytes.NewReader(body))
		request.Header.Set("X-Admin-Password", "test-password")
	}
	request.RemoteAddr = "127.0.0.1:30100"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func readCredentialAPI(t *testing.T, mux *http.ServeMux) credentialAPIView {
	t.Helper()
	response := callCredentialAPI(mux, nil)
	var view credentialAPIView
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil {
		t.Fatalf("credential status response invalid, HTTP %d", response.Code)
	}
	return view
}

func TestCredentialAppendAndDeletePreserveEffectivePool(t *testing.T) {
	const first = "fixture-config-first-Nw24LK"
	const second = "fixture-config-second-yU5hM3"
	const extra = "fixture-overlay-extra-j7Kpg6"
	load, _ := credentialFixture(t, "tavily:\n  sk_list: ["+first+", "+second+"]\n")
	t.Setenv("TAVILY_SK", "fixture-shadowed-env-HK49AM")
	mux := credentialMux(load())
	before := readCredentialAPI(t, mux)
	initial := before.Credentials["TAVILY_SK"]
	if initial.Count != 2 || !slices.Equal(initial.Sources, []string{"config"}) {
		t.Fatal("running pool does not reflect precedence of config list over environment")
	}
	appendKey := map[string]any{"name": "TAVILY_SK", "action": "append", "value": extra, "label": "Backup 1", "confirm": true}
	for range 2 {
		if res := callCredentialAPI(mux, appendKey); res.Code != http.StatusOK {
			t.Fatalf("append failed, HTTP %d", res.Code)
		}
	}
	staged := readCredentialAPI(t, mux)
	pending := staged.Pending["TAVILY_SK"]
	if !staged.PendingRestart || staged.Credentials["TAVILY_SK"].Count != 2 || pending.Count != 3 ||
		!slices.Equal(pending.Sources, []string{"config", "overlay"}) || pending.Slots[2].Label != "Backup 1" {
		t.Fatal("pending append was not staged separately or did not preserve the original pool")
	}
	deleteSlot := map[string]any{"name": "TAVILY_SK", "action": "delete", "slot_id": initial.Slots[0].ID, "confirm": true}
	if res := callCredentialAPI(mux, deleteSlot); res.Code != http.StatusOK {
		t.Fatalf("delete failed, HTTP %d", res.Code)
	}
	_ = callCredentialAPI(mux, deleteSlot)
	if keys := load().Tavily.EffectiveSKList(); !slices.Equal(keys, []string{second, extra}) {
		t.Fatal("restart did not apply only the requested append and deletion")
	}
}

func TestCredentialReplacePreservesSlotPositionAndSafeLabel(t *testing.T) {
	const first = "fixture-front-Ft87kJ"
	const middle = "fixture-middle-Mv29qA"
	const last = "fixture-end-Zr59xT"
	const replacement = "fixture-replacement-Rj26nS"
	const rotated = "fixture-rotation-Pw73bV"
	load, path := credentialFixture(t, "tavily:\n  sk_list: ["+first+", "+middle+", "+last+"]\n")
	mux := credentialMux(load())
	view := readCredentialAPI(t, mux)
	initial := view.Pending["TAVILY_SK"].Slots
	label := map[string]any{"name": "TAVILY_SK", "action": "label", "slot_id": initial[1].ID, "label": "Backup 2", "confirm": true}
	if response := callCredentialAPI(mux, label); response.Code != http.StatusOK {
		t.Fatalf("label fixture failed, HTTP %d", response.Code)
	}
	before := readCredentialAPI(t, mux)
	prior, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"name": "TAVILY_SK", "action": "replace", "slot_id": initial[1].ID, "value": replacement, "revision": view.Revision}
	check := func(want int) config.CredentialEditResult {
		t.Helper()
		response := callCredentialAPI(mux, request)
		if response.Code != want {
			t.Fatalf("replace returned HTTP %d, want %d", response.Code, want)
		}
		for _, secret := range []string{first, middle, last, replacement, rotated} {
			if bytes.Contains(response.Body.Bytes(), []byte(secret)) {
				t.Fatal("replace response contains credential material")
			}
		}
		var result config.CredentialEditResult
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal("invalid replace response")
		}
		return result
	}
	check(http.StatusConflict)
	delete(request, "revision")
	check(http.StatusConflict)
	request["revision"], request["value"] = before.Revision, first
	check(http.StatusBadRequest)
	request["value"], request["slot_id"] = replacement, "missing-slot"
	check(http.StatusConflict)
	request["slot_id"] = initial[1].ID
	if result := check(http.StatusOK); result.Saved || result.Applied || result.Backup ||
		!result.RestartRequired || result.BeforeCount != 3 || result.AfterCount != 3 || result.Revision != before.Revision {
		t.Fatal("preview did not report a revision-bound three-slot candidate")
	}
	if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, prior) {
		t.Fatal("rejected or previewed replacement changed the private overlay")
	}
	request["confirm"] = true
	if result := check(http.StatusOK); !result.Saved || !result.Applied || !result.Backup || !result.RestartRequired ||
		result.BeforeCount != 3 || result.AfterCount != 3 || result.Revision == before.Revision {
		t.Fatal("confirmed replacement did not commit one slot and advance the revision")
	}
	staged := readCredentialAPI(t, mux)
	metadata := callCredentialAPI(mux, nil).Body.Bytes()
	for _, secret := range []string{first, middle, last, replacement} {
		if bytes.Contains(metadata, []byte(secret)) {
			t.Fatal("credential status contains key material")
		}
	}
	pending := staged.Pending["TAVILY_SK"].Slots
	if len(pending) != 3 || pending[0] != initial[0] || pending[2] != initial[2] ||
		pending[1].Source != "overlay" || pending[1].Label != "Backup 2" || staged.Credentials["TAVILY_SK"].Slots[1] != initial[1] {
		t.Fatal("replacement changed a sibling, slot position, safe label, or running state")
	}
	if keys := load().Tavily.EffectiveSKList(); !slices.Equal(keys, []string{first, replacement, last}) {
		t.Fatal("restart did not apply the replacement in the original position")
	}
	request["slot_id"], request["revision"] = pending[1].ID, staged.Revision
	if result := check(http.StatusOK); result.Applied || result.Backup || result.Revision != staged.Revision {
		t.Fatal("replacing a slot with its current value should leave the file alone")
	}
	request["value"] = rotated
	check(http.StatusOK)
	if keys := load().Tavily.EffectiveSKList(); !slices.Equal(keys, []string{first, rotated, last}) {
		t.Fatal("replacing an overlay slot lost its position")
	}
	after := readCredentialAPI(t, mux)
	if after.Pending["TAVILY_SK"].Slots[1] != pending[1] {
		t.Fatal("an overlay replacement should preserve slot identity and label")
	}
	response := callCredentialAPI(mux, map[string]any{"name": "TAVILY_SK", "action": "delete", "slot_id": pending[1].ID, "revision": after.Revision, "confirm": true})
	if response.Code != http.StatusOK {
		t.Fatalf("deleting replacement failed, HTTP %d", response.Code)
	}
	if keys := load().Tavily.EffectiveSKList(); !slices.Equal(keys, []string{first, last}) {
		t.Fatal("deletion unexpectedly restored the replaced config value")
	}
	response = callCredentialAPI(mux, map[string]any{"name": "TAVILY_SK", "action": "reset", "confirm": true})
	if response.Code != http.StatusOK {
		t.Fatalf("reset after replacement failed, HTTP %d", response.Code)
	}
	if keys := load().Tavily.EffectiveSKList(); !slices.Equal(keys, []string{first, middle, last}) {
		t.Fatal("reset did not restore the original config pool")
	}
}

func TestCredentialCatalogEnvironmentMutationAndReadOnlyAuth(t *testing.T) {
	load, secretPath := credentialFixture(t, "mode: engine\n")
	for _, name := range credentialTestNames {
		t.Setenv(name, "fixture-env-"+name+"-D8ci29")
	}
	const auth = "fixture-read-only-auth-W39KmL"
	t.Setenv("WEBSEARCH_TOKEN", auth)
	mux := credentialMux(load())
	view := readCredentialAPI(t, mux)
	if len(view.Credentials) != 16 || view.PendingRestart || view.Revision == "" {
		t.Fatal("credential catalog or running baseline is incomplete")
	}
	authStatus := view.Credentials["WEBSEARCH_TOKEN"]
	if !authStatus.ReadOnly || authStatus.Count != 1 || len(authStatus.Slots) != 0 || !slices.Equal(authStatus.Sources, []string{"env"}) {
		t.Fatal("auth token did not expose only its read-only state")
	}
	for _, name := range []string{"WEBSEARCH_TOKEN", "auth_token"} {
		res := callCredentialAPI(mux, map[string]any{"name": name, "action": "set", "value": auth, "confirm": true})
		if res.Code != http.StatusBadRequest || bytes.Contains(res.Body.Bytes(), []byte(auth)) {
			t.Fatal("auth token write was not rejected safely")
		}
	}
	for index, name := range credentialTestNames {
		status := view.Credentials[name]
		if status.Count != 1 || !slices.Equal(status.Sources, []string{"env"}) || status.Multi != (index < 9) {
			t.Fatalf("incorrect environment credential metadata for %s", name)
		}
		action := "set"
		if status.Multi {
			action = "append"
		}
		res := callCredentialAPI(mux, map[string]any{"name": name, "action": action, "value": "fixture-overlay-" + name + "-L9Py6S", "confirm": true})
		if res.Code != http.StatusOK || bytes.Contains(res.Body.Bytes(), []byte("L9Py6S")) {
			t.Fatalf("could not save credential for %s safely", name)
		}
	}
	mux = credentialMux(load())
	view = readCredentialAPI(t, mux)
	if view.PendingRestart {
		t.Fatal("restart did not consume staged credentials")
	}
	for index, name := range credentialTestNames {
		status := view.Credentials[name]
		wantSources := []string{"overlay"}
		if index < 9 {
			wantSources = []string{"env", "overlay"}
		}
		if status.Count != len(wantSources) || !slices.Equal(status.Sources, wantSources) {
			t.Fatalf("reloaded credential sources incorrect for %s", name)
		}
		res := callCredentialAPI(mux, map[string]any{"name": name, "action": "delete", "slot_id": status.Slots[len(status.Slots)-1].ID, "confirm": true})
		if res.Code != http.StatusOK {
			t.Fatalf("could not delete credential for %s", name)
		}
	}
	mux = credentialMux(load())
	view = readCredentialAPI(t, mux)
	for index, name := range credentialTestNames {
		status := view.Credentials[name]
		if (index < 9 && (status.Count != 1 || !slices.Equal(status.Sources, []string{"env"}))) || (index >= 9 && status.Count != 0) {
			t.Fatalf("deletion did not take effect after restart for %s", name)
		}
	}
	backups, err := os.ReadDir(filepath.Join(filepath.Dir(secretPath), "dashboard-secrets-backups"))
	if err != nil || len(backups) == 0 {
		t.Fatal("credential edits did not preserve existing overlay bytes in a backup")
	}
}

func TestCredentialLegacyOverlayAndConflictDoNotExposeSecrets(t *testing.T) {
	const legacy = "fixture-legacy-overlay-nBA39k"
	const extra = "fixture-candidate-secret-V0Q5ZA"
	load, path := credentialFixture(t, "exa:\n  sk_list: [fixture-base-exa-7BPxKT]\n")
	old := []byte(`{"EXA_API_KEY":"` + legacy + `","future_credential":"fixture-unknown"}`)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	conf := load()
	if !slices.Equal(conf.Exa.EffectiveSKList(), []string{legacy}) {
		t.Fatal("old overlay must keep its effective precedence")
	}
	mux := credentialMux(conf)
	start := readCredentialAPI(t, mux)
	status := start.Credentials["EXA_API_KEY"]
	if status.Count != 1 || !slices.Equal(status.Sources, []string{"overlay"}) || status.Slots[0].ID == "" {
		t.Fatal("old overlay was not projected as one anonymous slot")
	}
	preview := map[string]any{"name": "EXA_API_KEY", "action": "clear", "confirm": false}
	if res := callCredentialAPI(mux, preview); res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"after_count":0`)) {
		t.Fatal("preview failed to project an empty pool")
	}
	if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, old) {
		t.Fatal("preview modified the old overlay")
	}
	appendKey := map[string]any{"name": "EXA_API_KEY", "action": "append", "value": extra, "confirm": true}
	if res := callCredentialAPI(mux, appendKey); res.Code != http.StatusOK {
		t.Fatal("append to an old overlay failed")
	}
	preview["confirm"], preview["revision"] = true, start.Revision
	if res := callCredentialAPI(mux, preview); res.Code != http.StatusConflict {
		t.Fatal("stale clear overwrote a concurrent append")
	}
	view := readCredentialAPI(t, mux)
	if view.Pending["EXA_API_KEY"].Count != 2 || view.Pending["EXA_API_KEY"].Slots[0].ID != status.Slots[0].ID {
		t.Fatal("migrating an old overlay invalidated the retained slot or lost the append")
	}
	for _, label := range []string{legacy, "previous nBA39k", "part V0Q5ZA"} {
		res := callCredentialAPI(mux, map[string]any{"name": "EXA_API_KEY", "action": "label", "slot_id": status.Slots[0].ID, "label": label, "confirm": true})
		if res.Code != http.StatusBadRequest || bytes.Contains(res.Body.Bytes(), []byte(label)) {
			t.Fatal("credential label accepted or echoed secret material")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/__admin/api/secrets", strings.NewReader(`{"`+legacy+`":1}`))
	request.RemoteAddr = "127.0.0.1:30100"
	request.Header.Set("X-Admin-Password", "test-password")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || bytes.Contains(response.Body.Bytes(), []byte("nBA39k")) {
		t.Fatal("invalid request echoed an unexpected JSON field name")
	}
	for _, response := range [][]byte{callCredentialAPI(mux, nil).Body.Bytes(), getJSON(t, mux, "/__admin/api/settings")} {
		for _, text := range []string{legacy, "nBA39k", extra, "V0Q5ZA", "fixture-unknown", "7BPxKT"} {
			if bytes.Contains(response, []byte(text)) {
				t.Fatal("read endpoint included credential material")
			}
		}
	}
	if latest, err := os.ReadFile(path); err != nil || !bytes.Contains(latest, []byte(`"future_credential":"fixture-unknown"`)) {
		t.Fatal("editing a legacy credential discarded an unrelated overlay entry")
	}
	preview["revision"] = view.Revision
	if res := callCredentialAPI(mux, preview); res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"backup":true`)) {
		t.Fatal("confirmed clear did not save with a private backup")
	}
	conf = load()
	if len(conf.Exa.EffectiveSKList()) != 0 {
		t.Fatal("clear did not suppress all sources after restarting")
	}
	if res := callCredentialAPI(credentialMux(conf), map[string]any{"name": "EXA_API_KEY", "action": "reset", "confirm": true}); res.Code != http.StatusOK {
		t.Fatal("reset failed")
	}
	if keys := load().Exa.EffectiveSKList(); !slices.Equal(keys, []string{"fixture-base-exa-7BPxKT"}) {
		t.Fatal("reset did not restore the underlying configuration")
	}
}

func TestCredentialParallelAppendsAndInvalidFile(t *testing.T) {
	const first = "fixture-concurrent-first-T8mb6V"
	const second = "fixture-concurrent-second-hE6PXa"
	load, path := credentialFixture(t, "mode: engine\n")
	mux := credentialMux(load())
	start := make(chan struct{})
	statuses := make(chan int, 2)
	for _, value := range []string{first, second} {
		go func() {
			<-start
			res := callCredentialAPI(mux, map[string]any{"name": "TINYFISH_API_KEY", "action": "append", "value": value, "confirm": true})
			statuses <- res.Code
		}()
	}
	close(start)
	for range 2 {
		if status := <-statuses; status != http.StatusOK {
			t.Fatalf("concurrent append failed with HTTP %d", status)
		}
	}
	keys := load().TinyFish.EffectiveSKList()
	if len(keys) != 2 || !slices.Contains(keys, first) || !slices.Contains(keys, second) {
		t.Fatal("concurrent append overwrote a sibling credential")
	}
	corrupt := []byte(`{"TINYFISH_API_KEY":"` + first)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	res := callCredentialAPI(mux, map[string]any{"name": "TINYFISH_API_KEY", "action": "append", "value": second, "confirm": true})
	if res.Code != http.StatusInternalServerError || bytes.Contains(res.Body.Bytes(), []byte("T8mb6V")) {
		t.Fatal("invalid overlay was accepted or its contents were exposed")
	}
	if check, err := os.ReadFile(path); err != nil || !bytes.Equal(check, corrupt) {
		t.Fatal("invalid overlay was discarded")
	}
}

func TestCredentialWriteRejectsPendingPathChange(t *testing.T) {
	load, path := credentialFixture(t, "mode: engine\n")
	conf := load()
	mux := credentialMux(conf)
	other := filepath.Join(filepath.Dir(path), "next.json")
	yaml := "dashboard:\n  secrets_path: " + filepath.ToSlash(other) + "\n"
	if err := os.WriteFile(conf.SettingsFilePaths().Overlay, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	res := callCredentialAPI(mux, map[string]any{"name": "JINA_API_KEY", "action": "set", "value": "fixture-path-drift", "confirm": true})
	if res.Code != http.StatusConflict {
		t.Fatalf("credential write to the previous path returned HTTP %d", res.Code)
	}
	for _, candidate := range []string{path, other} {
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			t.Fatal("stale credential write changed a file")
		}
	}
}
