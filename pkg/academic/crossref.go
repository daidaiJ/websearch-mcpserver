package academic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"websearch/pkg/antirobot"
)

var crossrefPath = "https://api.crossref.org/works"

// ──────────────────────────────────────────────────────────────────────────────
// Crossref 学术文献元数据（国内友好）
// ──────────────────────────────────────────────────────────────────────────────

type crossrefEngine struct {
	client *http.Client
}

// NewCrossref 创建 Crossref 引擎。client 为 nil 时使用默认客户端。
func NewCrossref(_ antirobot.CrossrefOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &crossrefEngine{client: client}
}

func (e *crossrefEngine) Name() string                    { return "crossref" }
func (e *crossrefEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *crossrefEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	offset := (page - 1) * 20
	if offset < 0 {
		offset = 0
	}

	u := crossrefURL(query, offset, timeRange)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch/1.0 (mailto:search@example.com)")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crossref request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("crossref HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

func crossrefURL(query string, offset int, timeRange antirobot.TimeRange) string {
	params := url.Values{
		"query":  {query},
		"offset": {fmt.Sprintf("%d", offset)},
	}
	if since := antirobot.TimeRangeSince(timeRange); since != "" {
		// Crossref expects date filters inside the `filter` parameter.
		params.Set("filter", "from-pub-date:"+since)
	}

	return crossrefPath + "?" + params.Encode()
}

// ── JSON 解析 ──

type crossrefResp struct {
	Message crossrefMessage `json:"message"`
}

type crossrefMessage struct {
	Items []crossrefItem `json:"items"`
}

type crossrefItem struct {
	Title     []string          `json:"title"`
	Container []string          `json:"container-title"`
	DOI       string            `json:"doi"`
	URL       string            `json:"URL"`
	Abstract  string            `json:"abstract"`
	Authors   []crossrefAuthor  `json:"author"`
	Published crossrefPublished `json:"published"`
	Subject   []string          `json:"subject"`
	Type      string            `json:"type"`
	Volume    string            `json:"volume"`
	Page      string            `json:"page"`
	ISSN      []string          `json:"ISSN"`
	Publisher string            `json:"publisher"`
	Score     float64           `json:"score"`
}

type crossrefAuthor struct {
	Given  string `json:"given"`
	Family string `json:"family"`
}

type crossrefPublished struct {
	DateParts [][]int `json:"date-parts"`
}

func (e *crossrefEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var resp crossrefResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("crossref parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(resp.Message.Items))
	for _, item := range resp.Message.Items {
		if item.Type == "component" {
			continue
		}

		title := ""
		if len(item.Title) > 0 {
			title = antirobot.CollapseSpace(strings.TrimSpace(item.Title[0]))
		}
		if title == "" {
			continue
		}

		journal := ""
		if len(item.Container) > 0 {
			journal = item.Container[0]
		}

		if item.Type == "book-chapter" && journal != "" && len(item.Title) > 0 {
			title = fmt.Sprintf("%s (%s)", journal, title)
		}

		authors := make([]string, 0, len(item.Authors))
		for _, a := range item.Authors {
			name := strings.TrimSpace(a.Given + " " + a.Family)
			if name != "" {
				authors = append(authors, name)
			}
		}

		pubDate := ""
		if len(item.Published.DateParts) > 0 && len(item.Published.DateParts[0]) > 0 {
			parts := item.Published.DateParts[0]
			switch len(parts) {
			case 3:
				pubDate = fmt.Sprintf("%04d-%02d-%02d", parts[0], parts[1], parts[2])
			case 2:
				pubDate = fmt.Sprintf("%04d-%02d", parts[0], parts[1])
			case 1:
				pubDate = fmt.Sprintf("%04d", parts[0])
			}
		}

		abstract := antirobot.CollapseSpace(strings.TrimSpace(item.Abstract))
		abstract = antirobot.StripXMLTags(abstract)

		resultURL := item.URL
		if resultURL == "" && item.DOI != "" {
			resultURL = "https://doi.org/" + item.DOI
		}

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         resultURL,
			Content:     abstract,
			Authors:     strings.Join(authors, ", "),
			PublishedAt: pubDate,
			DOI:         item.DOI,
			Journal:     journal,
			Score:       item.Score,
			Engine:      "crossref",
		})
	}

	return &antirobot.SearchResponse{Engine: "crossref", Results: results}, nil
}
