package bing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// OpenAlex 开放学术图谱（国内友好）
// ──────────────────────────────────────────────────────────────────────────────

type openalexEngine struct {
	client *http.Client
	mailTo string
}

// NewOpenAlex 创建 OpenAlex 引擎。
func NewOpenAlex(opts OpenAlexOpts) Engine {
	return &openalexEngine{
		client: &http.Client{Timeout: 15 * time.Second},
		mailTo: opts.MailTo,
	}
}

func (e *openalexEngine) Name() string         { return "openalex" }
func (e *openalexEngine) Region() NetworkRegion { return RegionChina }

func (e *openalexEngine) Search(query string, page int, timeRange TimeRange) (*SearchResponse, error) {
	params := url.Values{
		"search":   {query},
		"page":     {fmt.Sprintf("%d", page)},
		"per-page": {"10"},
		"sort":     {"relevance_score:desc"},
	}
	if e.mailTo != "" {
		params.Set("mailto", e.mailTo)
	}
	if since := TimeRangeSince(timeRange); since != "" {
		params.Set("filter", "from_publication_date:"+since)
	}

	u := "https://api.openalex.org/works?" + params.Encode()

	resp, err := e.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("openalex request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openalex HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

// ── JSON 解析 ──

type openalexResp struct {
	Results []openalexWork `json:"results"`
}

type openalexWork struct {
	Title          string               `json:"title"`
	DOI            string               `json:"doi"`
	ID             string               `json:"id"`
	DisplayName    string               `json:"display_name"`
	AbstractInvIdx map[string][]int     `json:"abstract_inverted_index"`
	Authorships    []openalexAuthorship `json:"authorships"`
	PrimaryLocation *openalexLocation   `json:"primary_location"`
	PublicationDate string              `json:"publication_date"`
	CitedByCount   int                  `json:"cited_by_count"`
	Keywords       []openalexConcept    `json:"keywords"`
}

type openalexAuthorship struct {
	Author openalexAuthor `json:"author"`
}

type openalexAuthor struct {
	DisplayName string `json:"display_name"`
}

type openalexLocation struct {
	Source      *openalexSource `json:"source"`
	LandingPage string          `json:"landing_page_url"`
	PDFUrl      string          `json:"pdf_url"`
}

type openalexSource struct {
	DisplayName string `json:"display_name"`
}

type openalexConcept struct {
	DisplayName string `json:"display_name"`
}

func (e *openalexEngine) parse(data []byte) (*SearchResponse, error) {
	var resp openalexResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("openalex parse: %w", err)
	}

	results := make([]Result, 0, len(resp.Results))
	for _, w := range resp.Results {
		title := w.Title
		if title == "" {
			title = w.DisplayName
		}
		if title == "" {
			continue
		}
		title = collapseSpace(strings.TrimSpace(title))

		resultURL := ""
		if w.PrimaryLocation != nil && w.PrimaryLocation.LandingPage != "" {
			resultURL = w.PrimaryLocation.LandingPage
		}
		if resultURL == "" && w.DOI != "" {
			resultURL = w.DOI
		}
		if resultURL == "" {
			resultURL = w.ID
		}

		pdfURL := ""
		if w.PrimaryLocation != nil && w.PrimaryLocation.PDFUrl != "" {
			pdfURL = w.PrimaryLocation.PDFUrl
		}

		abstract := reconstructAbstract(w.AbstractInvIdx)

		authors := make([]string, 0, len(w.Authorships))
		for _, a := range w.Authorships {
			if a.Author.DisplayName != "" {
				authors = append(authors, a.Author.DisplayName)
			}
		}

		journal := ""
		if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil {
			journal = w.PrimaryLocation.Source.DisplayName
		}

		doi := strings.TrimPrefix(w.DOI, "https://doi.org/")

		results = append(results, Result{
			Type:        ResultPaper,
			Title:       title,
			URL:         resultURL,
			Content:     abstract,
			PDFURL:      pdfURL,
			Authors:     strings.Join(authors, ", "),
			PublishedAt: w.PublicationDate,
			DOI:         doi,
			Journal:     journal,
			CitedBy:     w.CitedByCount,
			Engine:      "openalex",
		})
	}

	return &SearchResponse{Engine: "openalex", Results: results}, nil
}

func reconstructAbstract(invIdx map[string][]int) string {
	if len(invIdx) == 0 {
		return ""
	}
	maxPos := 0
	for _, positions := range invIdx {
		for _, p := range positions {
			if p > maxPos {
				maxPos = p
			}
		}
	}
	words := make([]string, maxPos+1)
	for token, positions := range invIdx {
		for _, p := range positions {
			if p < len(words) {
				words[p] = token
			}
		}
	}
	return collapseSpace(strings.Join(words, " "))
}
