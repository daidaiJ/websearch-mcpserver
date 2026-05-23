package bing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Semantic Scholar 学术搜索（海外优先）
// ──────────────────────────────────────────────────────────────────────────────

type semanticScholarEngine struct {
	client *http.Client
}

// NewSemanticScholar 创建 Semantic Scholar 引擎。
func NewSemanticScholar(_ SemanticScholarOpts) Engine {
	return &semanticScholarEngine{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (e *semanticScholarEngine) Name() string         { return "semantic_scholar" }
func (e *semanticScholarEngine) Region() NetworkRegion { return RegionInternational }

func (e *semanticScholarEngine) Search(query string, page int, timeRange TimeRange) (*SearchResponse, error) {
	offset := (page - 1) * 10
	if offset < 0 {
		offset = 0
	}

	params := url.Values{
		"query":  {query},
		"offset": {fmt.Sprintf("%d", offset)},
		"limit":  {"10"},
		"fields": {"title,url,abstract,authors,year,externalIds,venue,citationCount,openAccessPdf,relevanceScore"},
	}
	if since := TimeRangeSince(timeRange); since != "" {
		year := since[:4]
		params.Set("year", year+"-")
	}

	u := "https://api.semanticscholar.org/graph/v1/paper/search?" + params.Encode()

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch/1.0")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("semantic scholar request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("semantic scholar HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

// ── JSON 解析 ──

type ssResponse struct {
	Total int       `json:"total"`
	Data  []ssPaper `json:"data"`
}

type ssPaper struct {
	PaperID         string     `json:"paperId"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
	Abstract        string     `json:"abstract"`
	Year            int        `json:"year"`
	Venue           string     `json:"venue"`
	CitationCount   int        `json:"citationCount"`
	RelevanceScore  float64    `json:"relevanceScore"`
	Authors         []ssAuthor `json:"authors"`
	ExternalIDs     ssExtIDs   `json:"externalIds"`
	OpenAccess      *ssOA      `json:"openAccessPdf"`
}

type ssAuthor struct {
	Name string `json:"name"`
}

type ssExtIDs struct {
	DOI   string `json:"DOI"`
	ArXiv string `json:"ArXiv"`
}

type ssOA struct {
	URL string `json:"url"`
}

var ssVersionCache struct {
	sync.Once
	version string
}

func (e *semanticScholarEngine) fetchVersion() string {
	ssVersionCache.Do(func() {
		resp, err := e.client.Get("https://www.semanticscholar.org")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		re := regexp.MustCompile(`<meta\s+name="s2-ui-version"\s+content="([^"]+)"`)
		if m := re.FindSubmatch(body); len(m) > 1 {
			ssVersionCache.version = string(m[1])
		}
	})
	return ssVersionCache.version
}

func (e *semanticScholarEngine) parse(data []byte) (*SearchResponse, error) {
	var resp ssResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("semantic scholar parse: %w", err)
	}

	results := make([]Result, 0, len(resp.Data))
	for _, p := range resp.Data {
		if p.Title == "" {
			continue
		}

		resultURL := p.URL
		if resultURL == "" && p.ExternalIDs.DOI != "" {
			resultURL = "https://doi.org/" + p.ExternalIDs.DOI
		}

		pdfURL := ""
		if p.OpenAccess != nil && p.OpenAccess.URL != "" {
			pdfURL = p.OpenAccess.URL
		}

		authors := make([]string, 0, len(p.Authors))
		for _, a := range p.Authors {
			if a.Name != "" {
				authors = append(authors, a.Name)
			}
		}

		pubDate := ""
		if p.Year > 0 {
			pubDate = fmt.Sprintf("%d", p.Year)
		}

		title := collapseSpace(strings.TrimSpace(p.Title))
		abstract := collapseSpace(strings.TrimSpace(p.Abstract))

		results = append(results, Result{
			Type:        ResultPaper,
			Title:       title,
			URL:         resultURL,
			Content:     abstract,
			PDFURL:      pdfURL,
			Authors:     strings.Join(authors, ", "),
			PublishedAt: pubDate,
			DOI:         p.ExternalIDs.DOI,
			Journal:     p.Venue,
			CitedBy:     p.CitationCount,
			Score:       p.RelevanceScore,
			Engine:      "semantic_scholar",
		})
	}

	return &SearchResponse{Engine: "semantic_scholar", Results: results}, nil
}
