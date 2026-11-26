package sitefetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"websearch/pkg/i18n"
)

// Wikipedia article HTML contains navigation, edit links and infobox noise.
// The MediaWiki extract API returns the article prose directly, which keeps the
// cleanfetch body useful without a generic HTML-to-Markdown pass.

var wikiAPIURL = func(lang string) string {
	return fmt.Sprintf("https://%s.wikipedia.org/w/api.php", lang)
}

var wikiLangRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)

type WikipediaFetcher struct {
	client *http.Client
}

func NewWikipediaFetcher() *WikipediaFetcher {
	return &WikipediaFetcher{client: &http.Client{Timeout: 15 * time.Second}}
}

func (f *WikipediaFetcher) Provider() string { return "wikipedia_api" }

func (f *WikipediaFetcher) Match(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if _, ok := wikipediaLang(u.Hostname()); !ok {
		return false
	}
	return wikipediaTitle(u.Path) != ""
}

type wikiExtractResponse struct {
	Query struct {
		Pages []struct {
			Title   string `json:"title"`
			Extract string `json:"extract"`
			Missing bool   `json:"missing"`
		} `json:"pages"`
	} `json:"query"`
}

func (f *WikipediaFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	lang, ok := wikipediaLang(u.Hostname())
	if !ok {
		return "", errors.New(i18n.T("fetch.sitefetch.wikipedia_unsupported"))
	}
	title := wikipediaTitle(u.Path)
	if title == "" {
		return "", errors.New(i18n.T("fetch.sitefetch.wikipedia_unsupported"))
	}

	query := url.Values{}
	query.Set("action", "query")
	query.Set("prop", "extracts")
	query.Set("explaintext", "1")
	query.Set("exlimit", "1")
	query.Set("redirects", "1")
	query.Set("format", "json")
	query.Set("formatversion", "2")
	query.Set("titles", title)
	endpoint := wikiAPIURL(lang) + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "websearch-mcpserver/1.0 (Wikipedia fetch)")
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("fetch.sitefetch.request_failed", "Provider", "Wikipedia"), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", i18n.T("fetch.sitefetch.api_http", "Provider", "Wikipedia", "Status", resp.StatusCode))
	}
	var out wikiExtractResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Query.Pages) == 0 || out.Query.Pages[0].Missing {
		return "", errors.New(i18n.T("fetch.sitefetch.wikipedia_empty", "Title", title))
	}
	page := out.Query.Pages[0]
	extract := strings.TrimSpace(page.Extract)
	if extract == "" {
		return "", errors.New(i18n.T("fetch.sitefetch.wikipedia_empty", "Title", page.Title))
	}
	return fmt.Sprintf("# %s\n\n%s\n", strings.TrimSpace(page.Title), extract), nil
}

// wikipediaLang accepts language subdomains, including variants such as
// zh-min-nan, and the legacy mobile/host forms.
func wikipediaLang(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "wikipedia.org" || host == "www.wikipedia.org" {
		return "en", true
	}
	if !strings.HasSuffix(host, ".wikipedia.org") {
		return "", false
	}
	prefix := strings.TrimSuffix(host, ".wikipedia.org")
	prefix = strings.TrimSuffix(prefix, ".m")
	if prefix == "www" {
		return "en", true
	}
	if !wikiLangRe.MatchString(prefix) {
		return "", false
	}
	return prefix, true
}

// wikipediaTitle extracts the article title from /wiki/{title}. Percent
// decoding runs before underscore folding so encoded slashes stay literal.
func wikipediaTitle(path string) string {
	const prefix = "/wiki/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	raw := strings.TrimPrefix(path, prefix)
	title, err := url.PathUnescape(raw)
	if err != nil {
		return ""
	}
	title = strings.ReplaceAll(title, "_", " ")
	return strings.TrimSpace(title)
}
