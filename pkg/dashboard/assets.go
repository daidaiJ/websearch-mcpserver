package dashboard

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"websearch/pkg/i18n"
)

//go:embed web/dist web/logo-*.png
var webFiles embed.FS

// Vite content-addresses executable assets and fonts; the license files use stable names.
var versionedAsset = regexp.MustCompile(`^assets/[A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(js|css|woff2)$`)

const dashboardPath = "/dashboard/"
const dashboardBootstrapAnchor = `<script id="websearch-bootstrap"></script>`

func (h *Handler) registerDashboardAssets(mux *http.ServeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	dist, _ := fs.Sub(webFiles, "web/dist")
	mux.HandleFunc(strings.TrimSuffix(dashboardPath, "/"), guard(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dashboardPath, http.StatusTemporaryRedirect)
	}))
	mux.HandleFunc(dashboardPath, guard(i18n.HTTP(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			methodNotAllowed(w)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		name := strings.TrimPrefix(r.URL.Path, dashboardPath)
		switch name {
		case "":
			h.serveIndex(w, r, dist)
		case "index.html":
			http.Redirect(w, r, "./", http.StatusTemporaryRedirect)
		case "logo-green.png", "logo-blue.png", "logo-mono.png":
			http.ServeFileFS(w, r, webFiles, "web/"+name)
		default:
			if !strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
			info, err := fs.Stat(dist, name)
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			if versionedAsset.MatchString(name) {
				w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
				w.Header().Set("ETag", strconv.Quote(name))
			}
			http.ServeFileFS(w, r, dist, name)
		}
	})))
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request, dist fs.FS) {
	page, err := fs.ReadFile(dist, "index.html")
	if err != nil || !bytes.Contains(page, []byte(dashboardBootstrapAnchor)) {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}
	code := i18n.FromContext(r.Context()).Code()
	catalogs := map[string]json.RawMessage{
		i18n.Zh: i18n.RawMessages(i18n.Zh, "web"),
		i18n.En: i18n.RawMessages(i18n.En, "web"),
	}
	brand := brandView(h.conf)
	// The browser loads configured local logos through the guarded endpoint.
	if brand["logo"] != "/__admin/api/brand/logo" {
		brand["logo"] = ""
	}
	payload, err := json.Marshal(map[string]any{"catalogs": catalogs, "brand": brand, "locale": code})
	if err != nil {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}
	nonce := rand.Text()
	bootstrap := `<script nonce="` + nonce + `">(() => { const boot = ` + string(payload) + `;
` + dashboardBootstrap + `
})();</script>`
	page = bytes.Replace(page, []byte(dashboardBootstrapAnchor), []byte(bootstrap), 1)
	if code == i18n.En {
		page = bytes.Replace(page, []byte(`lang="zh-CN"`), []byte(`lang="en"`), 1)
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'nonce-"+nonce+"'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "Accept-Language")
	if r.Method == http.MethodGet {
		_, _ = w.Write(page)
	}
}

// Apply persisted preferences before the stylesheet or module can paint. The
// API and the app still start with the negotiated language when set to auto.
const dashboardBootstrap = `
window.__DEFAULT_LOCALE__ = boot.locale;
window.__I18N_CATALOGS__ = boot.catalogs;
window.__BRAND__ = boot.brand;
let theme, density, language, brand;
try {
  theme = localStorage.getItem('websearch.theme');
  density = localStorage.getItem('websearch.density');
  language = localStorage.getItem('websearch.language');
  brand = localStorage.getItem('websearch.brand');
} catch {}
const html = document.documentElement;
const locale = language === 'en' || language === 'zh' ? language : boot.locale;
window.__I18N__ = boot.catalogs[locale];
html.lang = locale === 'en' ? 'en' : 'zh-CN';
html.dataset.theme = theme === 'dark' || theme === 'light' ? theme
  : (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
html.dataset.density = density === 'compact' ? 'compact' : 'normal';
const brandOverride = brand === 'green' || brand === 'blue' || brand === 'mono';
html.dataset.brand = brandOverride ? brand : boot.brand.theme;
if (!brandOverride && boot.brand.accent) html.style.setProperty('--brand-accent', boot.brand.accent);
`
