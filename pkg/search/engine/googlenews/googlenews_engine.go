// Package googlenews Google News RSS 搜索引擎——独立新闻索引（零 Key），带结构化发布日期。
// 国内出口需代理（2026-10-06 实测：直连超时，代理 RSS 200 + 52 条 item）。
// RSS item 的 link 是 news.google.com 跳转 blob，必须解析回发布方 URL 才能
// 与普通网页结果去重合并（解析走 Google 自家客户端的 batchexecute RPC，
// 2026-10-06 本项目出口实测可回源；对齐上游 free-search-mcp gnews.py 契约）。
package googlenews

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
	"websearch/pkg/proxy"
)

// rssEndpoint 单测可指向 httptest 服务。
var rssEndpoint = "https://news.google.com/rss/search"

// ──────────────────────────────────────────────────────────────────────────────
// Google News RSS 引擎
// ──────────────────────────────────────────────────────────────────────────────

type gnewsEngine struct {
	opts    GoogleNewsOpts
	num     int
	limiter *antirobot.RateLimiter
	client  *http.Client
}

// GoogleNewsOpts Google News RSS 引擎配置。
type GoogleNewsOpts struct {
	Enabled      bool
	Edition      string              // 新闻版本（默认 zh-CN，见 editionParams）
	NumResults   int                 // 单次结果数（默认 10）
	PerSec       int                 // 每秒限流（默认 1）
	PerMin       int                 // 每分钟限流（默认 30）
	ProxyResolve proxy.ProxyResolver // 代理端点动态解析函数（每次请求实时获取）
}

// NewGoogleNews 创建 Google News 引擎（需代理访问）。
func NewGoogleNews(opts GoogleNewsOpts) antirobot.Engine {
	edition := strings.TrimSpace(opts.Edition)
	if _, ok := editionParams[edition]; !ok {
		edition = "zh-CN"
	}
	numResults := opts.NumResults
	if numResults <= 0 {
		numResults = 10
	}
	if numResults > 50 {
		numResults = 50
	}
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = 1
	}
	if perMin <= 0 {
		perMin = 30
	}
	return &gnewsEngine{
		opts:    opts,
		num:     numResults,
		limiter: antirobot.NewRateLimiter(perSec, perMin),
		client:  proxy.NewDynamicHTTPClient(opts.ProxyResolve, 15*time.Second),
	}
}

func (e *gnewsEngine) Name() string                    { return "googlenews" }
func (e *gnewsEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

// editionParams 常用新闻版本的 RSS 区域参数（hl 语言 / gl 地区 / ceid 版本标识）。
var editionParams = map[string][3]string{
	"zh-CN": {"zh-CN", "CN", "CN:zh-Hans"},
	"zh-TW": {"zh-TW", "TW", "TW:zh-Hant"},
	"en-US": {"en-US", "US", "US:en"},
	"en-GB": {"en-GB", "GB", "GB:en"},
	"ja-JP": {"ja", "JP", "JP:ja"},
	"ko-KR": {"ko", "KR", "KR:ko"},
}

// gnewsFreshnessMap Google News 的 when: 查询操作符。
var gnewsFreshnessMap = map[antirobot.TimeRange]string{
	antirobot.TimeRangeDay:   "when:1d",
	antirobot.TimeRangeWeek:  "when:7d",
	antirobot.TimeRangeMonth: "when:1m",
	antirobot.TimeRangeYear:  "when:1y",
}

// ── RSS XML 结构 ──

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

func (e *gnewsEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	// RSS 无翻页参数，第 2 页起返回空
	if page > 1 {
		return &antirobot.SearchResponse{Engine: "googlenews", Results: []antirobot.Result{}}, nil
	}
	if !e.limiter.Allow() {
		return &antirobot.SearchResponse{Engine: "googlenews", Results: []antirobot.Result{}}, nil
	}

	u := e.buildURL(query, timeRange)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	results := e.parseRSS(body)
	e.resolvePublisherURLs(results)
	return &antirobot.SearchResponse{Engine: "googlenews", Results: results}, nil
}

// buildURL 构造 RSS 检索 URL；freshness 映射为 when: 操作符拼进查询。
func (e *gnewsEngine) buildURL(query string, timeRange antirobot.TimeRange) string {
	p := editionParams[e.edition()]
	hl, gl, ceid := p[0], p[1], p[2]
	if when := gnewsFreshnessMap[timeRange]; when != "" {
		query = query + " " + when
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("hl", hl)
	q.Set("gl", gl)
	q.Set("ceid", ceid)
	return rssEndpoint + "?" + q.Encode()
}

func (e *gnewsEngine) edition() string {
	if _, ok := editionParams[e.opts.Edition]; ok {
		return e.opts.Edition
	}
	return "zh-CN"
}

func (e *gnewsEngine) parseRSS(body []byte) []antirobot.Result {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		log.Warnf("googlenews: RSS 解析失败: %v", err)
		return nil
	}
	results := make([]antirobot.Result, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}
		date, dateSource := "", ""
		if t := parseRSSTime(item.PubDate); !t.IsZero() {
			date = t.Format("2006-01-02")
			dateSource = antirobot.DateSourceStructured // RSS <pubDate> 为精确发布时间
		}
		results = append(results, antirobot.Result{
			Type:        antirobot.ResultWeb,
			Title:       title,
			URL:         link,
			Content:     stripHTML(item.Description),
			PublishedAt: date,
			DateSource:  dateSource,
			Engine:      "googlenews",
		})
	}
	return results
}

// parseRSSTime 解析 RSS RFC-2822 pubDate（GMT/数字时区两种形态）。
func parseRSSTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// stripHTML 去掉 RSS description 里的 HTML 标签并解码常见实体。
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	var sb strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			sb.WriteRune(r)
		}
	}
	return antirobot.CollapseSpace(htmlUnescape(sb.String()))
}

func htmlUnescape(s string) string {
	return strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
		"&#x27;", "'", "&#x2F;", "/",
	).Replace(s)
}
