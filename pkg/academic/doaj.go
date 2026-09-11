package academic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"websearch/pkg/antirobot"
)

// ──────────────────────────────────────────────────────────────────────────────
// DOAJ 开放获取期刊文章索引（国内友好，全学科 OA）
// ──────────────────────────────────────────────────────────────────────────────

// doajPath 单测可指向 httptest 服务。
var doajPath = "https://doaj.org/api/search/articles"

type doajEngine struct {
	client *http.Client
}

// NewDOAJ 创建 DOAJ 引擎。client 为 nil 时使用默认客户端。
func NewDOAJ(_ antirobot.DOAJOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &doajEngine{client: client}
}

func (e *doajEngine) Name() string                    { return "doaj" }
func (e *doajEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *doajEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	u := doajURL(query, page, timeRange)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch/1.0")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doaj request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("doaj HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

func doajURL(query string, page int, timeRange antirobot.TimeRange) string {
	q := query
	if since := antirobot.TimeRangeSince(timeRange); len(since) >= 4 {
		// DOAJ rejects wildcard queries. Its article records expose a numeric
		// `bibjson.year`, so use an explicit year range instead.
		q += fmt.Sprintf(" AND bibjson.year:[%s TO %d]", since[:4], time.Now().Year()+1)
	}
	return fmt.Sprintf("%s/%s?pageSize=10&page=%d", doajPath, url.PathEscape(q), page)
}

// ── JSON 解析 ──

type doajResp struct {
	Results []doajHit `json:"results"`
}

type doajHit struct {
	Bibjson doajBibjson `json:"bibjson"`
}

type doajIdentifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type doajLink struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type doajBibjson struct {
	Title    string `json:"title"`
	Abstract string `json:"abstract"`
	Year     string `json:"year"`
	Author   []struct {
		Name string `json:"name"`
	} `json:"author"`
	Identifier []doajIdentifier `json:"identifier"`
	Link       []doajLink       `json:"link"`
	Journal    struct {
		Title string `json:"title"`
	} `json:"journal"`
}

func (e *doajEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var resp doajResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("doaj parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(resp.Results))
	for _, hit := range resp.Results {
		b := hit.Bibjson
		title := antirobot.CollapseSpace(strings.TrimSpace(b.Title))
		if title == "" {
			continue
		}

		doi := ""
		for _, id := range b.Identifier {
			if id.Type == "doi" {
				doi = id.ID
				break
			}
		}
		fulltext := ""
		for _, l := range b.Link {
			if l.Type == "fulltext" {
				fulltext = l.URL
				break
			}
		}

		// URL：优先全文链接，回退 DOI 落地页（两者皆无则跳过，避免无 URL 的空结果）
		resultURL := fulltext
		if resultURL == "" && doi != "" {
			resultURL = "https://doi.org/" + doi
		}
		if resultURL == "" {
			continue
		}

		authors := make([]string, 0, len(b.Author))
		for _, a := range b.Author {
			if a.Name != "" {
				authors = append(authors, a.Name)
			}
		}

		pubDate := ""
		if y, err := strconv.Atoi(strings.TrimSpace(b.Year)); err == nil && y > 1900 && y < 2100 {
			pubDate = b.Year
		}

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         resultURL,
			Content:     antirobot.CollapseSpace(strings.TrimSpace(b.Abstract)),
			Authors:     strings.Join(authors, ", "),
			PublishedAt: pubDate,
			DOI:         doi,
			Journal:     antirobot.CollapseSpace(strings.TrimSpace(b.Journal.Title)),
			Engine:      "doaj",
		})
	}

	return &antirobot.SearchResponse{Engine: "doaj", Results: results}, nil
}
