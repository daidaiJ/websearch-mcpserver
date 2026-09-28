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
// Europe PMC 生物医学/生命科学文献（国内友好，PubMed 增补源）
// ──────────────────────────────────────────────────────────────────────────────

// europePMCPath 单测可指向 httptest 服务。
var europePMCPath = "https://www.ebi.ac.uk/europepmc/webservices/rest/search"

type europePMCEngine struct {
	client *http.Client
}

// NewEuropePMC 创建 Europe PMC 引擎。client 为 nil 时使用默认客户端。
func NewEuropePMC(_ antirobot.EuropePMCOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = defaultHTTPClient
	}
	return &europePMCEngine{client: client}
}

func (e *europePMCEngine) Name() string                    { return "europepmc" }
func (e *europePMCEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *europePMCEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	params := url.Values{
		"query":    {query},
		"format":   {"json"},
		"pageSize": {"10"},
		"page":     {fmt.Sprintf("%d", page)},
	}
	if since := antirobot.TimeRangeSince(timeRange); since != "" {
		// Europe PMC 日期范围语法：PUB_YEAR 逐年匹配用 OR 太啰嗦，用 >= 起始日期
		params.Set("query", query+" AND (FIRST_PDATE:["+since+" TO *])")
	}

	u := europePMCPath + "?" + params.Encode()

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch/1.0")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("europe pmc request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("europe pmc HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

// ── JSON 解析 ──

type europePMCResp struct {
	ResultList struct {
		Result []europePMCItem `json:"result"`
	} `json:"resultList"`
}

type europePMCItem struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Title  string `json:"title"`
	AuthorString string `json:"authorString"`
	DOI    string `json:"doi"`
	JournalInfo struct {
		Journal struct {
			Title string `json:"title"`
		} `json:"journal"`
	} `json:"journalInfo"`
	CitedByCount         int    `json:"citedByCount"`
	FirstPublicationDate string `json:"firstPublicationDate"`
	IsOpenAccess         string `json:"isOpenAccess"`
}

func (e *europePMCEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var resp europePMCResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("europe pmc parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(resp.ResultList.Result))
	for _, item := range resp.ResultList.Result {
		title := antirobot.CollapseSpace(strings.TrimSpace(item.Title))
		if title == "" {
			continue
		}

		journal := antirobot.CollapseSpace(strings.TrimSpace(item.JournalInfo.Journal.Title))

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         europePMCURL(item.DOI, item.Source, item.ID),
			Content:     "", // 列表接口无摘要
			Authors:     antirobot.CollapseSpace(strings.TrimSuffix(strings.TrimSpace(item.AuthorString), ".")),
			PublishedAt: item.FirstPublicationDate,
			DOI:         item.DOI,
			Journal:     journal,
			CitedBy:     item.CitedByCount,
			Engine:      "europepmc",
		})
	}

	return &antirobot.SearchResponse{Engine: "europepmc", Results: results}, nil
}

// europePMCURL 有 DOI 时用解析站落地页；否则用 source/id 文章页（必须逐条唯一，
// 否则 URL 去重会把多条结果撞成一条）。
func europePMCURL(doi, source, id string) string {
	if doi != "" {
		return "https://doi.org/" + doi
	}
	if source != "" && id != "" {
		return fmt.Sprintf("https://europepmc.org/article/%s/%s", source, id)
	}
	return ""
}
