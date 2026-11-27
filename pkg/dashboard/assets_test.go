package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func testDashboardMux(conf config.Config) *http.ServeMux {
	h := New(conf, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !IsLoopbackRemote(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r)
		}
	})
	return mux
}

func getDashboardPath(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:8192"
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	return resp
}

func TestDashboardServesExternalUIIntegration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<!doctype html><html lang="zh-CN"><head><script id="websearch-bootstrap"></script><script type="module" src="./static/app.js"></script></head><body>external-dashboard</body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "static", "app.js"), []byte("console.log('external')"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := config.Config{}
	conf.Dashboard.UI.Dir = dir
	mux := testDashboardMux(conf)

	page := getDashboardPath(t, mux, dashboardPath)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "external-dashboard") {
		t.Fatalf("external index is not served: %d %s", page.Code, page.Body.String())
	}
	nonce := regexp.MustCompile(`<script nonce="([^"]+)">`).FindStringSubmatch(page.Body.String())
	if len(nonce) != 2 || !strings.Contains(page.Header().Get("Content-Security-Policy"), "nonce-"+nonce[1]) {
		t.Fatal("external index did not receive a CSP nonce for the bootstrap")
	}
	if !strings.Contains(page.Body.String(), "__DEFAULT_LOCALE__") {
		t.Fatal("external index bootstrap anchor was not replaced")
	}
	asset := getDashboardPath(t, mux, dashboardPath+"static/app.js")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "external") {
		t.Fatalf("external asset is not served: %d", asset.Code)
	}
	if dotfile := getDashboardPath(t, mux, dashboardPath+".env"); dotfile.Code != http.StatusNotFound {
		t.Fatalf("dotfile must not be exposed: %d", dotfile.Code)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("host-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.txt")); err == nil {
		if escaped := getDashboardPath(t, mux, dashboardPath+"escape.txt"); escaped.Code != http.StatusNotFound {
			t.Fatalf("symlink escaping ui.dir must not be served: %d", escaped.Code)
		}
	}
	for _, name := range []string{"../index.html", "/etc/passwd", `static\app.js`, "."} {
		if validDashboardResource(name) {
			t.Errorf("unsafe resource name accepted: %q", name)
		}
	}
}

func TestDashboardExternalIndexSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("host-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "index.html")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	conf := config.Config{}
	conf.Dashboard.UI.Dir = dir
	resp := getDashboardPath(t, testDashboardMux(conf), dashboardPath)
	if resp.Code != http.StatusInternalServerError || strings.Contains(resp.Body.String(), "host-secret") {
		t.Fatalf("index symlink escaping ui.dir must fail closed: %d %q", resp.Code, resp.Body.String())
	}
}

func TestDashboardExternalUINeedsIndex(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.UI.Dir = t.TempDir()
	resp := getDashboardPath(t, testDashboardMux(conf), dashboardPath)
	if resp.Code != http.StatusInternalServerError || !strings.Contains(resp.Body.String(), "index.html") {
		t.Fatalf("missing external index must fail closed: %d %s", resp.Code, resp.Body.String())
	}
}

func TestDashboardExternalUIServesPlainIndex(t *testing.T) {
	dir := t.TempDir()
	index := `<!doctype html><html lang="fr"><body>no-bootstrap</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := config.Config{}
	conf.Dashboard.UI.Dir = dir
	resp := getDashboardPath(t, testDashboardMux(conf), dashboardPath)
	if resp.Code != http.StatusOK || resp.Body.String() != index {
		t.Fatalf("external index without a bootstrap anchor must be served unchanged: %d %q", resp.Code, resp.Body.String())
	}
}

func TestDashboardUIDisabledKeepsAPI(t *testing.T) {
	off := false
	conf := config.Config{}
	conf.Dashboard.UI.Enabled = &off
	conf.Dashboard.UI.Dir = filepath.Join(t.TempDir(), "missing")
	mux := testDashboardMux(conf)
	if resp := getDashboardPath(t, mux, dashboardPath); resp.Code != http.StatusNotFound {
		t.Fatalf("disabled UI must not serve /dashboard/: %d", resp.Code)
	}
	if resp := getDashboardPath(t, mux, "/__admin/api/storage"); resp.Code != http.StatusOK {
		t.Fatalf("disabled UI must keep the API available: %d", resp.Code)
	}
}

func TestDashboardAPIResponseCarriesVersion(t *testing.T) {
	resp := getDashboardPath(t, testDashboardMux(config.Config{}), "/__admin/api/storage")
	if resp.Code != http.StatusOK {
		t.Fatalf("storage endpoint failed: %d %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("X-WebSearch-Dashboard-API"); got != "1" {
		t.Fatalf("API version header = %q, want 1", got)
	}
}

func TestDashboardServesEmbeddedApp(t *testing.T) {
	if !dashboardUIEmbedded {
		t.Skip("no_ui build has no embedded frontend")
	}
	conf := config.Config{}
	conf.Dashboard.Brand.Title = `Example </script><script>unsafeTitle</script>`
	conf.Dashboard.Brand.Theme = "blue"
	conf.Dashboard.Brand.Accent = "#ABC"
	conf.Dashboard.Brand.Logo = filepath.Join(t.TempDir(), "local-logo.png")
	logo, err := webFiles.ReadFile("web/logo-blue.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf.Dashboard.Brand.Logo, logo, 0o600); err != nil {
		t.Fatal(err)
	}
	h := New(conf, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !IsLoopbackRemote(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r)
		}
	})
	get := func(path string, lang string, local bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Language", lang)
		if local {
			req.RemoteAddr = "127.0.0.1:8192"
		}
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		return resp
	}
	page := get(dashboardPath, "en-US", true)
	if page.Code != http.StatusOK || page.Header().Get("Vary") != "Accept-Language" || page.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("unexpected dashboard response: %d, %v", page.Code, page.Header())
	}
	body := page.Body.String()
	inlineScript := regexp.MustCompile(`<script nonce="([A-Z2-7]+)">\(\(\) => \{ const boot = (.+);\n`).FindStringSubmatch(body)
	if !strings.Contains(body, `<html lang="en"`) || len(inlineScript) != 3 || strings.Contains(body, conf.Dashboard.Brand.Title) {
		t.Fatal("negotiated language or escaped bootstrap missing")
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self' 'nonce-"+inlineScript[1]+"'") {
		t.Fatal("bootstrap nonce is missing from Content-Security-Policy")
	}
	var boot struct {
		Locale   string                                `json:"locale"`
		Brand    map[string]any                        `json:"brand"`
		Catalogs map[string]map[string]json.RawMessage `json:"catalogs"`
	}
	if err := json.Unmarshal([]byte(inlineScript[2]), &boot); err != nil {
		t.Fatal(err)
	}
	if boot.Locale != "en" || len(boot.Catalogs["en"]) == 0 || len(boot.Catalogs["zh"]) == 0 ||
		boot.Brand["title"] != conf.Dashboard.Brand.Title || boot.Brand["theme"] != "blue" || boot.Brand["accent"] != "#aabbcc" ||
		boot.Brand["logo"] != "/__admin/api/brand/logo" || boot.Brand["footer"] != true {
		t.Fatalf("unexpected dashboard bootstrap locale or brand: %q, %#v", boot.Locale, boot.Brand)
	}
	if zh := get(dashboardPath, "zh-CN", true); zh.Code != http.StatusOK || !strings.Contains(zh.Body.String(), `<html lang="zh-CN"`) {
		t.Fatal("Chinese request must preserve the Chinese HTML language")
	}
	var embeddedPaths []string
	base := &url.URL{Path: dashboardPath}
	for _, match := range regexp.MustCompile(`<(?:link|script)\b[^>]*(?:src|href)="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		reference, err := url.Parse(match[1])
		if err != nil || reference.IsAbs() || reference.Host != "" {
			t.Fatalf("nonlocal asset reference %q", match[1])
		}
		path := base.ResolveReference(reference).Path
		if strings.HasPrefix(path, dashboardPath+"assets/") {
			embeddedPaths = append(embeddedPaths, path)
		}
		if resp := get(path, "en", true); resp.Code != http.StatusOK || resp.Body.Len() == 0 {
			t.Errorf("embedded asset %s returned %d", path, resp.Code)
		}
	}
	if len(embeddedPaths) == 0 {
		t.Fatal("dashboard HTML does not reference its embedded assets")
	}
	for _, path := range embeddedPaths {
		asset := get(path, "en", true)
		if !strings.Contains(asset.Header().Get("Cache-Control"), "immutable") || asset.Header().Get("ETag") == "" {
			t.Fatalf("hashed resource must be re-usable: %s", path)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:8192"
		req.Header.Set("If-None-Match", asset.Header().Get("ETag"))
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotModified || resp.Body.Len() != 0 {
			t.Fatalf("expected conditional 304 for %s: %d", path, resp.Code)
		}
	}
	const brandLogoPath = "/__admin/api/brand/logo"
	for _, path := range []string{strings.TrimSuffix(dashboardPath, "/"), dashboardPath, embeddedPaths[0], dashboardPath + "logo-blue.png", brandLogoPath} {
		if resp := get(path, "en", false); resp.Code != http.StatusForbidden {
			t.Errorf("dashboard path %s did not use the read guard: %d", path, resp.Code)
		}
	}
	if redirect := get("/dashboard", "en", true); redirect.Code != http.StatusTemporaryRedirect || redirect.Header().Get("Location") != dashboardPath {
		t.Fatalf("dashboard entry without a trailing slash did not redirect: %d %s", redirect.Code, redirect.Header().Get("Location"))
	}
	for _, path := range []string{"logo-green.png", "logo-blue.png", "logo-mono.png"} {
		if resp := get(dashboardPath+path, "en", true); resp.Code != http.StatusOK || resp.Header().Get("Content-Type") != "image/png" {
			t.Errorf("built-in logo %s is unavailable: %d", path, resp.Code)
		}
	}
	if custom := get(brandLogoPath, "en", true); custom.Code != http.StatusOK || custom.Header().Get("Content-Type") != "image/png" || custom.Body.String() != string(logo) {
		t.Errorf("configured local logo is unavailable: %d", custom.Code)
	}
	for _, path := range []string{"next/", "dist/", "app.css", "app.js"} {
		if resp := get(dashboardPath+path, "en", true); resp.Code != http.StatusNotFound {
			t.Errorf("removed dashboard path %s returned %d", path, resp.Code)
		}
	}
}
