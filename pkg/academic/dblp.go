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
// DBLP 计算机科学会议/期刊文献索引（国内友好）
// ──────────────────────────────────────────────────────────────────────────────

// dblpPath 单测可指向 httptest 服务。
var dblpPath = "https://dblp.org/search/publ/api"

type dblpEngine struct {
	client  *http.Client
	limiter *antirobot.RateLimiter
}

// NewDBLP 创建 DBLP 引擎。client 为 nil 时使用默认客户端。
// 无强制限流，仍按礼貌访问惯例套 2/s、30/min 限流器。
func NewDBLP(_ antirobot.DBLPOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = defaultHTTPClient
	}
	return &dblpEngine{
		client:  client,
		limiter: antirobot.NewRateLimiter(2, 30),
	}
}

func (e *dblpEngine) Name() string                    { return "dblp" }
func (e *dblpEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *dblpEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	offset := (page - 1) * 10
	if offset < 0 {
		offset = 0
	}

	// 限流等待
	for !e.limiter.Allow() {
		time.Sleep(500 * time.Millisecond)
	}

	params := url.Values{
		"q":      {query},
		"format": {"json"},
		"h":      {"10"},
		"f":      {fmt.Sprintf("%d", offset)},
	}
	if since := antirobot.TimeRangeSince(timeRange); len(since) >= 4 {
		// TimeRangeSince 固定 "2006-01-02" 格式，取前 4 位即年份
		params.Set("q", query+" year:"+since[:4]+"..")
	}

	u := dblpPath + "?" + params.Encode()

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch/1.0")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dblp request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("dblp HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return e.parse(body)
}

// ── JSON 解析 ──

type dblpResp struct {
	Result struct {
		Hits struct {
			Hit []struct {
				Info dblpInfo `json:"info"`
			} `json:"hit"`
		} `json:"hits"`
	} `json:"result"`
}

type dblpInfo struct {
	Authors json.RawMessage `json:"authors"` // 单作者为对象、多作者为数组
	Title   string          `json:"title"`
	Venue   string          `json:"venue"`
	Year    string          `json:"year"`
	DOI     string          `json:"doi"`
	EE      string          `json:"ee"`
	URL     string          `json:"url"`
}

// dblpAuthorNames 解析 authors 字段：兼容 {"author":[{"text":..}]}（多作者）、
// {"author":{"text":..}}（单作者）、裸数组三种形态。
func dblpAuthorNames(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var wrapper struct {
		Author json.RawMessage `json:"author"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Author) > 0 {
		raw = wrapper.Author
	}

	var one struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &one); err == nil && one.Text != "" {
		return one.Text
	}
	var many []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &many); err == nil {
		names := make([]string, 0, len(many))
		for _, m := range many {
			if m.Text != "" {
				names = append(names, m.Text)
			}
		}
		return strings.Join(names, ", ")
	}
	return ""
}

func (e *dblpEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var resp dblpResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("dblp parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(resp.Result.Hits.Hit))
	for _, h := range resp.Result.Hits.Hit {
		info := h.Info
		title := antirobot.CollapseSpace(strings.TrimSpace(info.Title))
		// DBLP 标题以 . 结尾，去除避免展示突兀
		title = strings.TrimSuffix(title, ".")
		if title == "" {
			continue
		}

		// URL 优先电子版链接（通常即 DOI 落地页）
		resultURL := info.EE
		if resultURL == "" {
			resultURL = info.URL
		}
		if resultURL == "" && info.DOI != "" {
			resultURL = "https://doi.org/" + info.DOI
		}

		pubDate := ""
		if y, err := strconv.Atoi(strings.TrimSpace(info.Year)); err == nil && y > 1900 && y < 2100 {
			pubDate = info.Year
		}

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         resultURL,
			Content:     "", // DBLP 无摘要
			Authors:     dblpAuthorNames(info.Authors),
			PublishedAt: pubDate,
			DateSource:  antirobot.DateSourceStructured,
			DOI:         info.DOI,
			Journal:     antirobot.CollapseSpace(strings.TrimSpace(info.Venue)),
			Engine:      "dblp",
		})
	}

	return &antirobot.SearchResponse{Engine: "dblp", Results: results}, nil
}
