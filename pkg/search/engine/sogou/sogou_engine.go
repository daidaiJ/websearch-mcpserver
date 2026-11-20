// Package sogou 搜狗网页搜索（sogou.com）——中文通用索引，HTML 抓取，零 Key。
// 解析契约对齐上游 searxng/searx/engines/sogou.py（2026-11-20 出口实测：
// 直连 200 + 自然结果，div.vrwrap 结构；无验证码）。
package sogou

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"websearch/pkg/antirobot"

	"github.com/PuerkitoBio/goquery"
)

const sogouBaseURL = "https://www.sogou.com/web"

var sogouTimeRange = map[antirobot.TimeRange]string{
	antirobot.TimeRangeDay:   "inttime_day",
	antirobot.TimeRangeWeek:  "inttime_week",
	antirobot.TimeRangeMonth: "inttime_month",
	antirobot.TimeRangeYear:  "inttime_year",
}

var (
	sogouDateRe    = regexp.MustCompile(`(\d{4})[-年/](\d{1,2})[-月/](\d{1,2})`)
	sogouBlockedRe = regexp.MustCompile(`antispider|id="seccode"|请输入验证码`)
)

type sogouEngine struct {
	opts     SogouOpts
	endpoint string // 测试可覆写
	limiter  *antirobot.RateLimiter
	client   *http.Client

	mu          sync.Mutex
	backoff     time.Duration
	consecFails int
	stateEpoch  uint64
}

var sogouUAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

const sogouMaxBackoff = 60 * time.Second

// 前置退避等待参数改为变量：单测需要缩短（生产取值不变）。
var (
	sogouBaseDelay = 300 * time.Millisecond
	sogouJitter    = 500 * time.Millisecond
)

func (e *sogouEngine) Name() string                    { return "sogou" }
func (e *sogouEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *sogouEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	return e.SearchContext(context.Background(), query, page, timeRange)
}

// SearchContext 实现 antirobot.ContextEngine。
func (e *sogouEngine) SearchContext(ctx context.Context, query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !e.limiter.Allow() {
		return antirobot.LocalLimitSkipped("sogou")
	}

	gen := e.stateGen()

	if err := e.preDelay(ctx); err != nil {
		return nil, err
	}
	u := e.buildURL(query, page, timeRange)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	e.setHeaders(req)

	resp, err := e.client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, err
	}
	html := string(body)

	if resp.StatusCode != 200 {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if isSogouBlocked(resp, html) {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, fmt.Errorf("blocked by Sogou anti-bot")
	}

	if ctx.Err() == nil {
		e.recordSuccess(gen)
	}
	results := e.parseResults(html)

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}
	return &antirobot.SearchResponse{Engine: "sogou", Results: results}, nil
}

var _ antirobot.ContextEngine = (*sogouEngine)(nil)

func (e *sogouEngine) stateGen() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateEpoch
}

func (e *sogouEngine) buildURL(query string, page int, timeRange antirobot.TimeRange) string {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("query", e.applyBlocked(query))
	q.Set("page", strconv.Itoa(page))
	if tr := sogouTimeRange[timeRange]; tr != "" {
		q.Set("s_from", tr)
		q.Set("tsn", "1")
	}
	base := e.endpoint
	if base == "" {
		base = sogouBaseURL
	}
	return base + "?" + q.Encode()
}

func (e *sogouEngine) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", sogouUAs[rand.Intn(len(sogouUAs))])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Referer", "https://www.sogou.com/")
}

func (e *sogouEngine) preDelay(ctx context.Context) error {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := sogouBaseDelay + time.Duration(rand.Int63n(int64(sogouJitter)))
	if bo > 0 {
		delay += bo
	}
	return antirobot.SleepCtx(ctx, delay)
}

func (e *sogouEngine) recordSuccess(since uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stateEpoch != since {
		return
	}
	e.backoff = 0
	e.consecFails = 0
}

func (e *sogouEngine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	e.stateEpoch++
	bo := sogouBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > sogouMaxBackoff {
		bo = sogouMaxBackoff
	}
	e.backoff = bo
}

// isSogouBlocked 识别反爬跳转/验证码页。搜狗通常 302 到 /antispider/。
func isSogouBlocked(resp *http.Response, html string) bool {
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		if strings.Contains(strings.ToLower(resp.Request.URL.String()), "antispider") {
			return true
		}
	}
	return sogouBlockedRe.MatchString(html)
}

// ── HTML 解析 ──

func (e *sogouEngine) parseResults(htmlText string) []antirobot.Result {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	var results []antirobot.Result
	doc.Find("div.rb, div.vrwrap").Each(func(_ int, sel *goquery.Selection) {
		if sel.Find("div.special-wrap").Length() > 0 {
			return
		}
		link := sel.Find("h3.pt a, h3.vr-title a").First()
		if link.Length() == 0 {
			return
		}
		title := antirobot.CollapseSpace(strings.TrimSpace(link.Text()))
		href := strings.TrimSpace(link.AttrOr("href", ""))

		// /link?url= 是点击包装；data-url 才是真实目标。
		if strings.HasPrefix(href, "/link?url=") || strings.Contains(href, "sogou.com/link") {
			if du, ok := sel.Find("[data-url]").First().Attr("data-url"); ok {
				href = strings.TrimSpace(du)
			}
		}
		if strings.HasPrefix(href, "/") {
			href = "https://www.sogou.com" + href
		}
		if href == "" || title == "" || !strings.HasPrefix(href, "http") || isSogouLinkWrap(href) {
			return
		}

		var content string
		for _, selector := range []string{
			"div.ft",
			"div.attribute-centent",
			"div.fz-mid.space-txt",
			`div[class*="summary"]`,
		} {
			if text := antirobot.CollapseSpace(strings.TrimSpace(sel.Find(selector).First().Text())); text != "" {
				content = text
				break
			}
		}

		publishedAt, dateSource := "", ""
		if m := sogouDateRe.FindStringSubmatch(sel.Find("span.cite-date, cite").First().Text()); m != nil {
			year, _ := strconv.Atoi(m[1])
			month, _ := strconv.Atoi(m[2])
			day, _ := strconv.Atoi(m[3])
			publishedAt = fmt.Sprintf("%04d-%02d-%02d", year, month, day)
			dateSource = antirobot.DateSourceStructured
		}

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultWeb,
			Title:       title,
			URL:         href,
			Content:     content,
			PublishedAt: publishedAt,
			DateSource:  dateSource,
			Engine:      "sogou",
		})
	})
	return results
}

func isSogouLinkWrap(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "sogou.com" || host == "www.sogou.com") && strings.HasPrefix(u.Path, "/link")
}

// ── 站点屏蔽 ──

func (e *sogouEngine) applyBlocked(query string) string {
	if len(e.opts.Blocked) > 5 {
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

func (e *sogouEngine) filterBlocked(results []antirobot.Result) []antirobot.Result {
	blocked := make(map[string]struct{}, len(e.opts.Blocked))
	for _, d := range e.opts.Blocked {
		blocked[strings.ToLower(d)] = struct{}{}
	}
	filtered := make([]antirobot.Result, 0, len(results))
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

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}
