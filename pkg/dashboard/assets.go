package dashboard

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"websearch/pkg/i18n"
)

// Vite content-addresses executable assets and fonts; the license files use stable names.
var versionedAsset = regexp.MustCompile(`^assets/[A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(js|css|woff2)$`)

const dashboardPath = "/dashboard/"
const dashboardBootstrapAnchor = `<script id="websearch-bootstrap"></script>`

// dashboardUI is the static root selected at registration time. An empty
// external directory means the embedded build; a configured but invalid
// directory fails closed instead of silently serving a different UI.
type dashboardUI struct {
	root     fs.FS
	dir      string
	external bool
	err      error
}

func (h *Handler) resolveDashboardUI() dashboardUI {
	dir := h.conf.GetDashboardUIDir()
	if dir == "" {
		if !dashboardUIEmbedded {
			return dashboardUI{err: fmt.Errorf("dashboard UI is not embedded in this build; set dashboard.ui.dir or build without the no_ui tag")}
		}
		dist, err := fs.Sub(webFiles, "web/dist")
		return dashboardUI{root: dist, err: err}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dashboardUI{err: fmt.Errorf("dashboard.ui.dir %q is invalid: %w", dir, err)}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return dashboardUI{err: fmt.Errorf("dashboard.ui.dir %q is unavailable: %w", dir, err)}
	}
	info, err := os.Stat(filepath.Join(resolved, "index.html"))
	if err != nil || !info.Mode().IsRegular() {
		return dashboardUI{err: fmt.Errorf("dashboard.ui.dir %q does not contain index.html", dir)}
	}
	return dashboardUI{root: os.DirFS(resolved), dir: resolved, external: true}
}

func (h *Handler) registerDashboardAssets(mux *http.ServeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	if !h.conf.Dashboard.UI.IsEnabled() {
		return
	}
	ui := h.resolveDashboardUI()
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
		if ui.err != nil {
			http.Error(w, ui.err.Error(), http.StatusInternalServerError)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, dashboardPath)
		switch name {
		case "":
			h.serveIndex(w, r, ui)
		case "index.html":
			http.Redirect(w, r, "./", http.StatusTemporaryRedirect)
		case "logo-green.png", "logo-blue.png", "logo-mono.png":
			http.ServeFileFS(w, r, webFiles, "web/"+name)
		default:
			h.serveDashboardAsset(w, r, ui, name)
		}
	})))
}

func (h *Handler) serveDashboardAsset(w http.ResponseWriter, r *http.Request, ui dashboardUI, name string) {
	if !validDashboardResource(name) {
		http.NotFound(w, r)
		return
	}
	if !ui.external && !strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}
	if ui.external && !withinDashboardDir(ui.dir, name) {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(ui.root, name)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if versionedAsset.MatchString(name) {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.Header().Set("ETag", strconv.Quote(name))
	}
	http.ServeFileFS(w, r, ui.root, name)
}

// validDashboardResource rejects traversal, absolute paths, backslashes and
// dotfiles. os.DirFS already enforces valid fs paths; this keeps accidental
// private files (for example .env) out of the public UI directory.
func validDashboardResource(name string) bool {
	if name == "" || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}

// withinDashboardDir keeps symlinks inside the configured UI directory. The
// directory is operator-controlled, but a writable mount must not be able to
// publish arbitrary host files through the dashboard origin.
func withinDashboardDir(root, name string) bool {
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request, ui dashboardUI) {
	if ui.external && !withinDashboardDir(ui.dir, "index.html") {
		http.Error(w, "index.html escapes dashboard.ui.dir", http.StatusInternalServerError)
		return
	}
	page, err := fs.ReadFile(ui.root, "index.html")
	if err != nil {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}
	code := i18n.FromContext(r.Context()).Code()
	nonce := rand.Text()
	hasBootstrap := bytes.Contains(page, []byte(dashboardBootstrapAnchor))
	if !hasBootstrap && !ui.external {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}
	if hasBootstrap {
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
		bootstrap := `<script nonce="` + nonce + `">(() => { const boot = ` + string(payload) + `;
` + dashboardBootstrap + `
})();</script>`
		page = bytes.Replace(page, []byte(dashboardBootstrapAnchor), []byte(bootstrap), 1)
		if code == i18n.En {
			page = bytes.Replace(page, []byte(`lang="zh-CN"`), []byte(`lang="en"`), 1)
		}
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'nonce-"+nonce+"'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
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
