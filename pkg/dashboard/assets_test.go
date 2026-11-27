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

func TestDashboardServesEmbeddedApp(t *testing.T) {
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
