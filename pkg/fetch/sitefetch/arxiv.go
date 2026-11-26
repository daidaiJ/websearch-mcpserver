package sitefetch

import (
	"context"
	"encoding/xml"
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

// arXiv abstract pages are readable HTML, but the export API gives a stable
// title/authors/abstract/category record and avoids the page's sidebar, nav and
// "report number" chrome.

var arxivAPIEndpoint = "https://export.arxiv.org/api/query"

// arxivIDRe accepts both the post-2007 numeric form and the older archive form.
var arxivIDRe = regexp.MustCompile(`^(\d{4}\.\d{4,5}|[a-z-]+(?:\.[A-Za-z]{2})?/\d{7})(v\d+)?$`)

type ArxivFetcher struct {
	client *http.Client
}

func NewArxivFetcher() *ArxivFetcher {
	return &ArxivFetcher{client: &http.Client{Timeout: 15 * time.Second}}
}

func (f *ArxivFetcher) Provider() string { return "arxiv_api" }

func (f *ArxivFetcher) Match(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "arxiv.org" && host != "www.arxiv.org" {
		return false
	}
	_, ok := arxivID(u.Path)
	return ok
}

// arxivID extracts the identifier from /abs/{id} and /html/{id}. PDF URLs stay
// on the PDF branch so cleanfetch can parse the document instead of the abstract.
func arxivID(path string) (string, bool) {
	for _, prefix := range []string{"/abs/", "/html/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		id := strings.Trim(strings.TrimPrefix(path, prefix), "/")
		if arxivIDRe.MatchString(id) {
			return id, true
		}
	}
	return "", false
}

type arxivFeed struct {
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID         string `xml:"id"`
	Title      string `xml:"title"`
	Summary    string `xml:"summary"`
	Published  string `xml:"published"`
	Updated    string `xml:"updated"`
	DOI        string `xml:"doi"`
	JournalRef string `xml:"journal_ref"`
	Comment    string `xml:"comment"`
	Authors    []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Categories []struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
}

func (f *ArxivFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	id, ok := arxivID(u.Path)
	if !ok {
		return "", errors.New(i18n.T("fetch.sitefetch.arxiv_unsupported"))
	}

	query := url.Values{}
	query.Set("id_list", id)
	query.Set("max_results", "1")
	endpoint := strings.TrimRight(arxivAPIEndpoint, "/") + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "websearch-mcpserver/1.0 (arXiv fetch)")
	req.Header.Set("Accept", "application/atom+xml")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("fetch.sitefetch.request_failed", "Provider", "arXiv"), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", i18n.T("fetch.sitefetch.api_http", "Provider", "arXiv", "Status", resp.StatusCode))
	}
	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return "", err
	}
	if len(feed.Entries) == 0 {
		return "", errors.New(i18n.T("fetch.sitefetch.arxiv_empty", "ID", id))
	}
	entry := feed.Entries[0]
	if strings.EqualFold(strings.TrimSpace(entry.Title), "Error") {
		return "", errors.New(i18n.T("fetch.sitefetch.arxiv_empty", "ID", id))
	}
	return renderArxivEntry(entry), nil
}

func renderArxivEntry(entry arxivEntry) string {
	title := strings.Join(strings.Fields(entry.Title), " ")
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", title)

	authors := make([]string, 0, len(entry.Authors))
	for _, author := range entry.Authors {
		if name := strings.TrimSpace(author.Name); name != "" {
			authors = append(authors, name)
		}
	}
	if len(authors) > 0 {
		sb.WriteString(i18n.T("fetch.sitefetch.arxiv_authors", "Authors", strings.Join(authors, ", ")))
		sb.WriteString("\n\n")
	}
	if date := arxivDate(entry.Published); date != "" {
		sb.WriteString(i18n.T("fetch.sitefetch.arxiv_published", "Date", date))
		sb.WriteString("\n\n")
	}
	categories := make([]string, 0, len(entry.Categories))
	seen := make(map[string]bool, len(entry.Categories))
	for _, cat := range entry.Categories {
		term := strings.TrimSpace(cat.Term)
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		categories = append(categories, term)
	}
	if len(categories) > 0 {
		sb.WriteString(i18n.T("fetch.sitefetch.arxiv_categories", "Categories", strings.Join(categories, ", ")))
		sb.WriteString("\n\n")
	}
	if doi := strings.TrimSpace(entry.DOI); doi != "" {
		sb.WriteString(i18n.T("fetch.sitefetch.arxiv_doi", "DOI", doi))
		sb.WriteString("\n\n")
	}
	if journal := strings.TrimSpace(entry.JournalRef); journal != "" {
		sb.WriteString(i18n.T("fetch.sitefetch.arxiv_journal", "Journal", journal))
		sb.WriteString("\n\n")
	}
	sb.WriteString(i18n.T("fetch.sitefetch.arxiv_abstract"))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(entry.Summary))
	sb.WriteString("\n")
	return sb.String()
}

func arxivDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}
