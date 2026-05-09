package bing

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// arXiv 预印本搜索（海外优先）
// ──────────────────────────────────────────────────────────────────────────────

type arxivEngine struct {
	client *http.Client
}

// NewArxiv 创建 arXiv 引擎。
func NewArxiv(_ ArxivOpts) Engine {
	return &arxivEngine{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (e *arxivEngine) Name() string         { return "arxiv" }
func (e *arxivEngine) Region() NetworkRegion { return RegionChina }

func (e *arxivEngine) Search(query string, page int, timeRange TimeRange) (*SearchResponse, error) {
	offset := (page - 1) * 10
	if offset < 0 {
		offset = 0
	}

	q := "all:" + query
	if since := TimeRangeSince(timeRange); since != "" {
		q += " AND submittedDate:[" + since + " TO *]"
	}

	u := fmt.Sprintf("https://export.arxiv.org/api/query?search_query=%s&start=%d&max_results=10",
		url.QueryEscape(q), offset)

	resp, err := e.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("arxiv request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("arxiv HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

// ── XML 解析 ──

type arxivFeed struct {
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	Title     string      `xml:"title"`
	ID        string      `xml:"id"`
	Summary   string      `xml:"summary"`
	Published string      `xml:"published"`
	Authors   []arxivAuthor `xml:"author"`
	Links     []arxivLink   `xml:"link"`
	DOI       string      `xml:"doi"`
	Category  []arxivCat  `xml:"category"`
	Comment   string      `xml:"comment"`
}

type arxivAuthor struct {
	Name string `xml:"name"`
}

type arxivLink struct {
	Href  string `xml:"href,attr"`
	Title string `xml:"title,attr"`
	Type  string `xml:"type,attr"`
}

type arxivCat struct {
	Term string `xml:"term,attr"`
}

func (e *arxivEngine) parse(data []byte) (*SearchResponse, error) {
	var feed arxivFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("arxiv parse: %w", err)
	}

	results := make([]Result, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		if entry.ID == "" {
			continue
		}

		authors := make([]string, 0, len(entry.Authors))
		for _, a := range entry.Authors {
			if a.Name != "" {
				authors = append(authors, a.Name)
			}
		}

		pdfURL := ""
		for _, lnk := range entry.Links {
			if lnk.Title == "pdf" {
				pdfURL = lnk.Href
				break
			}
		}

		title := collapseSpace(strings.TrimSpace(entry.Title))
		summary := collapseSpace(strings.TrimSpace(entry.Summary))

		results = append(results, Result{
			Type:        ResultPaper,
			Title:       title,
			URL:         entry.ID,
			Content:     summary,
			PDFURL:      pdfURL,
			Authors:     strings.Join(authors, ", "),
			PublishedAt: entry.Published[:min(len(entry.Published), 10)],
			DOI:         entry.DOI,
			Engine:      "arxiv",
		})
	}

	return &SearchResponse{Engine: "arxiv", Results: results}, nil
}
