package academic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"websearch/pkg/antirobot"
)

// ──────────────────────────────────────────────────────────────────────────────
// OpenAlex 开放学术图谱（国内友好）
// ──────────────────────────────────────────────────────────────────────────────

type openalexEngine struct {
	client *http.Client
	mailTo string
}

// NewOpenAlex 创建 OpenAlex 引擎。client 为 nil 时使用默认客户端。
func NewOpenAlex(opts antirobot.OpenAlexOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = defaultHTTPClient
	}
	return &openalexEngine{
		client: client,
		mailTo: opts.MailTo,
	}
}

func (e *openalexEngine) Name() string                    { return "openalex" }
func (e *openalexEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *openalexEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	params := url.Values{
		"search":   {query},
		"page":     {fmt.Sprintf("%d", page)},
		"per-page": {"10"},
		"sort":     {"relevance_score:desc"},
	}
	if e.mailTo != "" {
		params.Set("mailto", e.mailTo)
	}
	if since := antirobot.TimeRangeSince(timeRange); since != "" {
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
	BestOALocation  *openalexLocation   `json:"best_oa_location"`
	OpenAccess      *openalexOA         `json:"open_access"`
	PublicationDate string              `json:"publication_date"`
	CitedByCount   int                  `json:"cited_by_count"`
	RelevanceScore float64              `json:"relevance_score"`
	Keywords       []openalexConcept    `json:"keywords"`
}

type openalexAuthorship struct {
	Author openalexAuthor `json:"author"`
}

type openalexAuthor struct {
	DisplayName string `json:"display_name"`
}

type openalexOA struct {
	IsOA  bool   `json:"is_oa"`
	OAURL string `json:"oa_url"`
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

func (e *openalexEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var resp openalexResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("openalex parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(resp.Results))
	for _, w := range resp.Results {
		if r, ok := openalexWorkToResult(w); ok {
			results = append(results, r)
		}
	}

	return &antirobot.SearchResponse{Engine: "openalex", Results: results}, nil
}

func openalexPDFURL(w openalexWork) string {
	if w.PrimaryLocation != nil && w.PrimaryLocation.PDFUrl != "" {
		return w.PrimaryLocation.PDFUrl
	}
	if w.BestOALocation != nil && w.BestOALocation.PDFUrl != "" {
		return w.BestOALocation.PDFUrl
	}
	if w.OpenAccess != nil && w.OpenAccess.OAURL != "" {
		return w.OpenAccess.OAURL
	}
	return ""
}

func openalexWorkToResult(w openalexWork) (antirobot.Result, bool) {
	title := w.Title
	if title == "" {
		title = w.DisplayName
	}
	if title == "" {
		return antirobot.Result{}, false
	}
	title = antirobot.CollapseSpace(strings.TrimSpace(title))

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

	journal := ""
	if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil {
		journal = w.PrimaryLocation.Source.DisplayName
	}

	authors := make([]string, 0, len(w.Authorships))
	for _, a := range w.Authorships {
		if a.Author.DisplayName != "" {
			authors = append(authors, a.Author.DisplayName)
		}
	}

	doi := strings.TrimPrefix(w.DOI, "https://doi.org/")
	return antirobot.Result{
		Type:        antirobot.ResultPaper,
		Title:       title,
		URL:         resultURL,
		Content:     reconstructAbstract(w.AbstractInvIdx),
		PDFURL:      openalexPDFURL(w),
		Authors:     strings.Join(authors, ", "),
		PublishedAt: w.PublicationDate,
		DOI:         doi,
		Journal:     journal,
		CitedBy:     w.CitedByCount,
		Score:       w.RelevanceScore,
		Engine:      "openalex",
	}, true
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
	return antirobot.CollapseSpace(strings.Join(words, " "))
}
