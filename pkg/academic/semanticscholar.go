package academic

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
)

// ──────────────────────────────────────────────────────────────────────────────
// Semantic Scholar 学术搜索（海外优先）
// ──────────────────────────────────────────────────────────────────────────────

// 退避时长与 API 端点抽为包级变量，单测可缩短等待/指向 httptest 服务。
var (
	ssSearchEndpoint = "https://api.semanticscholar.org/graph/v1/paper/search"
	ssRetryBackoff   = 2 * time.Second // 带 key 首次 429/503 退避（叠加 rand 0~1s）
	ssDegradedWait   = 1 * time.Second // 降级匿名后最后一次重试前的等待
)

type semanticScholarEngine struct {
	client      *http.Client
	apiKey      string
	keyDisabled atomic.Bool // 连续 429 后降级为匿名，进程内不再回切（并发搜索共享实例，须原子访问）
}

// NewSemanticScholar 创建 Semantic Scholar 引擎。client 为 nil 时使用默认客户端。
func NewSemanticScholar(opts antirobot.SemanticScholarOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = defaultHTTPClient
	}
	return &semanticScholarEngine{client: client, apiKey: opts.APIKey}
}

func (e *semanticScholarEngine) Name() string { return "semantic_scholar" }
func (e *semanticScholarEngine) Region() antirobot.NetworkRegion {
	return antirobot.RegionInternational
}

func (e *semanticScholarEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
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
	if since := antirobot.TimeRangeSince(timeRange); since != "" {
		year := since[:4]
		params.Set("year", year+"-")
	}

	u := ssSearchEndpoint + "?" + params.Encode()

	// 重试策略（受 Searcher.execOne 默认 10s per-engine 超时约束，退避预算 ≤ 4s）：
	//   带 key:   429/503 → 退避 2~3s 重试 → 仍 429/503 则永久降级匿名，等 1s 最后重试一次
	//   无 key:   429/503 直接报错（匿名共享配额短窗内不会恢复）
	// proxy 层 retryTransport 对 429 已按 Retry-After 自动重试一次，两者叠加后
	// 单引擎最多 3 次引擎内请求，勿再加大。
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "websearch/1.0")
		if e.apiKey != "" && !e.keyDisabled.Load() {
			req.Header.Set("x-api-key", e.apiKey)
		}

		resp, err := e.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("semantic scholar request: %w", err)
		}
		if resp.StatusCode == 200 {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			return e.parse(body)
		}
		resp.Body.Close()

		if resp.StatusCode != 429 && resp.StatusCode != 503 {
			return nil, fmt.Errorf("semantic scholar HTTP %d", resp.StatusCode)
		}
		if e.apiKey == "" || e.keyDisabled.Load() {
			return nil, fmt.Errorf("semantic scholar HTTP %d", resp.StatusCode)
		}
		if attempt == 0 {
			time.Sleep(ssRetryBackoff + time.Duration(rand.Int63n(int64(time.Second))))
			continue
		}
		// 第二次仍限流：降级为匿名模式（engine 为进程级长生命周期单例，
		// 并发下最坏多打一次带 key 请求，无害）
		e.keyDisabled.Store(true)
		log.Warnf("semantic scholar: API key 连续 429，进程内降级为匿名模式（不再回切）")
		time.Sleep(ssDegradedWait)
	}
}

// ── JSON 解析 ──

type ssResponse struct {
	Total int       `json:"total"`
	Data  []ssPaper `json:"data"`
}

type ssPaper struct {
	PaperID        string     `json:"paperId"`
	Title          string     `json:"title"`
	URL            string     `json:"url"`
	Abstract       string     `json:"abstract"`
	Year           int        `json:"year"`
	Venue          string     `json:"venue"`
	CitationCount  int        `json:"citationCount"`
	RelevanceScore float64    `json:"relevanceScore"`
	Authors        []ssAuthor `json:"authors"`
	ExternalIDs    ssExtIDs   `json:"externalIds"`
	OpenAccess     *ssOA      `json:"openAccessPdf"`
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

func (e *semanticScholarEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var resp ssResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("semantic scholar parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(resp.Data))
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

		title := antirobot.CollapseSpace(strings.TrimSpace(p.Title))
		abstract := antirobot.CollapseSpace(strings.TrimSpace(p.Abstract))

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         resultURL,
			Content:     abstract,
			PDFURL:      pdfURL,
			Authors:     strings.Join(authors, ", "),
			PublishedAt: pubDate,
			DateSource:  antirobot.DateSourceStructured,
			DOI:         p.ExternalIDs.DOI,
			Journal:     p.Venue,
			CitedBy:     p.CitationCount,
			Score:       p.RelevanceScore,
			Engine:      "semantic_scholar",
		})
	}

	return &antirobot.SearchResponse{Engine: "semantic_scholar", Results: results}, nil
}
