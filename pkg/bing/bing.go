package bing

import (
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ──────────────────────────────────────────────────────────────────────────────
// Bing 通用网页搜索（国内友好）
// ──────────────────────────────────────────────────────────────────────────────

type bingEngine struct {
	opts    BingOpts
	limiter *RateLimiter

	mu          sync.Mutex
	client      *http.Client
	ua          string
	reqCount    int
	backoff     time.Duration
	consecFails int
}

var bingUAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
}

var bingAcceptVariants = []string{
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
}

var bingLangVariants = []string{
	"zh-CN,zh;q=0.9,en;q=0.8",
	"zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
}

var bingCountRe = regexp.MustCompile(`[^0-9]`)
var bingSafesearchMap = map[int]string{0: "off", 1: "moderate", 2: "strict"}

const (
	bingBaseDelay       = 500 * time.Millisecond
	bingJitter          = 800 * time.Millisecond
	bingMaxBackoff      = 60 * time.Second
	bingSessionLifetime = 30
	bingBlockThreshold  = 5
)

// NewBing 创建 Bing 引擎。
func NewBing(opts BingOpts) Engine {
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = 1
	}
	if perMin <= 0 {
		perMin = 20
	}
	e := &bingEngine{
		opts:    opts,
		limiter: NewRateLimiter(perSec, perMin),
	}
	e.rotateSession()
	return e
}

func (e *bingEngine) Name() string         { return "bing" }
func (e *bingEngine) Region() NetworkRegion { return RegionChina }

func (e *bingEngine) Search(query string, page int, timeRange TimeRange) (*SearchResponse, error) {
	if !e.limiter.Allow() {
		return &SearchResponse{Engine: "bing", Results: []Result{}}, nil
	}

	e.preDelay()
	u := e.buildURL(query, page, timeRange)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	e.setHeaders(req)

	resp, err := e.client.Do(req)
	if err != nil {
		e.recordFail()
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.recordFail()
		return nil, err
	}
	html := string(body)

	if resp.StatusCode == 429 || resp.StatusCode == 403 {
		e.recordFail()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		e.recordFail()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(html, `id="b_results"`) {
		e.recordFail()
		return nil, fmt.Errorf("blocked by anti-bot")
	}

	e.recordSuccess()
	results := e.parseResults(html)

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}

	e.mu.Lock()
	e.reqCount++
	rotate := e.reqCount >= bingSessionLifetime
	e.mu.Unlock()
	if rotate {
		e.rotateSession()
	}

	return &SearchResponse{Engine: "bing", Results: results}, nil
}

// ── 反爬防御 ──

func (e *bingEngine) preDelay() {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := bingBaseDelay + time.Duration(rand.Int63n(int64(bingJitter)))
	if bo > 0 {
		delay += bo
	}
	time.Sleep(delay)
}

func (e *bingEngine) rotateSession() {
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar, Timeout: 15 * time.Second}
	e.ua = bingUAs[rand.Intn(len(bingUAs))]
	e.reqCount = 0
}

func (e *bingEngine) setHeaders(req *http.Request) {
	e.mu.Lock()
	ua := e.ua
	e.mu.Unlock()
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", bingAcceptVariants[rand.Intn(len(bingAcceptVariants))])
	req.Header.Set("Accept-Language", bingLangVariants[rand.Intn(len(bingLangVariants))])
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-GPC", "1")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Referer", "https://www.bing.com/")
}

func (e *bingEngine) recordSuccess() {
	e.mu.Lock()
	e.backoff = 0
	e.consecFails = 0
	e.mu.Unlock()
}

func (e *bingEngine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	bo := bingBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > bingMaxBackoff {
		bo = bingMaxBackoff
	}
	e.backoff = bo
}

// ── URL 构造 ──

func (e *bingEngine) buildURL(query string, page int, timeRange TimeRange) string {
	q := url.Values{}
	q.Set("q", e.applyBlocked(query))
	q.Set("adlt", bingSafesearchMap[e.opts.SafeSearch])
	if page > 1 {
		q.Set("first", strconv.Itoa((page-1)*10+1))
	}
	if ex8 := bingTimeRangeCode(timeRange); ex8 != "" {
		q.Set("ex8", ex8)
	}
	return "https://www.bing.com/search?" + q.Encode()
}

func bingTimeRangeCode(tr TimeRange) string {
	switch tr {
	case TimeRangeDay:
		return "14745"
	case TimeRangeWeek:
		return "14747"
	case TimeRangeMonth:
		return "14748"
	case TimeRangeYear:
		return "14749"
	default:
		return ""
	}
}

func (e *bingEngine) applyBlocked(query string) string {
	if len(e.opts.Blocked) > bingBlockThreshold {
		return query
	}
	var sb strings.Builder
	sb.WriteString(query)
	for _, d := range e.opts.Blocked {
		sb.WriteString(" -site:")
		sb.WriteString(d)
	}
	return sb.String()
}

// ── HTML 解析 ──

func (e *bingEngine) parseResults(htmlText string) []Result {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	var results []Result
	doc.Find("ol#b_results li.b_algo").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find("h2 a").First()
		if link.Length() == 0 {
			return
		}
		title := strings.TrimSpace(link.Text())
		href, _ := link.Attr("href")
		if href == "" || title == "" {
			return
		}
		href = decodeBingURL(href)

		contentSel := sel.Find("p")
		contentSel.Find("span.algoSlug_icon").Remove()
		content := collapseSpace(strings.TrimSpace(contentSel.Text()))

		date := ""
		if ds := sel.Find("span.news_dt, span.ftrP").First(); ds.Length() > 0 {
			date = strings.TrimSpace(ds.Text())
		}

		results = append(results, Result{
			Type: ResultWeb, Title: title, URL: href,
			Content: content, PublishedAt: date, Engine: "bing",
		})
	})
	return results
}

// ── 站点屏蔽 ──

func (e *bingEngine) filterBlocked(results []Result) []Result {
	blocked := make(map[string]struct{}, len(e.opts.Blocked))
	for _, d := range e.opts.Blocked {
		blocked[strings.ToLower(d)] = struct{}{}
	}
	filtered := make([]Result, 0, len(results))
	for _, r := range results {
		host := extractHost(r.URL)
		hit := false
		for d := range blocked {
			if host == d || strings.HasSuffix(host, "."+d) {
				hit = true
				break
			}
		}
		if !hit {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// ── 辅助 ──

func decodeBingURL(href string) string {
	if !strings.HasPrefix(href, "https://") && !strings.HasPrefix(href, "http://") {
		return href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	if (parsed.Host == "cn.bing.com" || parsed.Host == "www.bing.com") && parsed.Path == "/ck/a" {
		uVal := parsed.Query().Get("u")
		if uVal == "" || !strings.HasPrefix(uVal, "a1") {
			return href
		}
		encoded := uVal[2:]
		if mod := len(encoded) % 4; mod != 0 {
			encoded += strings.Repeat("=", 4-mod)
		}
		decoded, err := base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return href
		}
		return string(decoded)
	}
	return href
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}

func collapseSpace(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}
