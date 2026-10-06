// Package so360 360 搜索（so.com）通用网页引擎——中文第二索引，国内直连可用。
// 解析契约与上游实测对齐（free-search-mcp so360.py，2026-09-21 实测；
// 本项目出口复测 2026-10-06：直连与代理均返回 200 + 自然结果）：
// 自然结果在 li.res-list，h3 a 锚点的 data-mdurl 携带真实目标 URL（href 为
// so.com/link 点击包装）；div.g-mohe 特型卡（短视频/商品聚合）不是自然结果。
package so360

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"websearch/pkg/antirobot"

	"github.com/PuerkitoBio/goquery"
)

// ──────────────────────────────────────────────────────────────────────────────
// 360 搜索（so.com）——中文网页搜索，HTML 抓取，无 API Key
// ──────────────────────────────────────────────────────────────────────────────

type so360Engine struct {
	opts    So360Opts
	limiter *antirobot.RateLimiter

	mu          sync.Mutex
	backoff     time.Duration
	consecFails int
}

var so360UAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

const (
	so360BaseDelay  = 500 * time.Millisecond
	so360Jitter     = 800 * time.Millisecond
	so360MaxBackoff = 60 * time.Second
)

func (e *so360Engine) Name() string                    { return "so360" }
func (e *so360Engine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *so360Engine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	if !e.limiter.Allow() {
		return &antirobot.SearchResponse{Engine: "so360", Results: []antirobot.Result{}}, nil
	}

	e.preDelay()
	u := e.buildURL(query, page, timeRange)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	e.setHeaders(req)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
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

	if resp.StatusCode != 200 {
		e.recordFail()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// 验证码页（wappass）= 反爬拦截；普通空结果页不含验证码标记，正常解析返回空
	if so360CaptchaRe.MatchString(html) {
		e.recordFail()
		return nil, fmt.Errorf("blocked by anti-bot")
	}

	e.recordSuccess()
	results := e.parseResults(html)

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}
	return &antirobot.SearchResponse{Engine: "so360", Results: results}, nil
}

// ── 反爬防御 ──

func (e *so360Engine) preDelay() {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := so360BaseDelay + time.Duration(rand.Int63n(int64(so360Jitter)))
	if bo > 0 {
		delay += bo
	}
	time.Sleep(delay)
}

func (e *so360Engine) recordSuccess() {
	e.mu.Lock()
	e.backoff = 0
	e.consecFails = 0
	e.mu.Unlock()
}

func (e *so360Engine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	bo := so360BaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > so360MaxBackoff {
		bo = so360MaxBackoff
	}
	e.backoff = bo
}

func (e *so360Engine) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", so360UAs[rand.Intn(len(so360UAs))])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Referer", "https://www.so.com/")
}

// ── URL 构造 ──

func (e *so360Engine) buildURL(query string, page int, timeRange antirobot.TimeRange) string {
	q := url.Values{}
	q.Set("q", e.applyBlocked(query))
	// 360 按每页 10 条返回；rn 请求条数（钳制到 10-50，过小按 10 处理）
	q.Set("rn", "10")
	if e.opts.SafeSearch > 0 {
		q.Set("secure", "1")
	}
	if tr := so360TimeRangeCode(timeRange); tr != "" {
		q.Set("adv_t", tr)
	}
	if page > 1 {
		q.Set("pn", fmt.Sprintf("%d", page))
	}
	return "https://www.so.com/s?" + q.Encode()
}

// so360TimeRangeCode 360 的高级时间范围参数 adv_t。
func so360TimeRangeCode(tr antirobot.TimeRange) string {
	switch tr {
	case antirobot.TimeRangeDay:
		return "d"
	case antirobot.TimeRangeWeek:
		return "w"
	case antirobot.TimeRangeMonth:
		return "m"
	case antirobot.TimeRangeYear:
		return "y"
	default:
		return ""
	}
}

func (e *so360Engine) applyBlocked(query string) string {
	if len(e.opts.Blocked) == 0 || len(e.opts.Blocked) > 5 {
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

func (e *so360Engine) parseResults(htmlText string) []antirobot.Result {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	var results []antirobot.Result
	doc.Find("li.res-list").Each(func(_ int, sel *goquery.Selection) {
		// g-mohe 特型卡（短视频聚合/商品卡等运营位）不是自然结果
		if sel.Find("div.g-mohe").Length() > 0 {
			return
		}
		link := sel.Find("h3 a").First()
		if link.Length() == 0 {
			return
		}
		title := antirobot.CollapseSpace(strings.TrimSpace(link.Text()))
		// data-mdurl 为真实目标 URL；href 常是 so.com/link 点击包装
		href := ""
		if md, ok := link.Attr("data-mdurl"); ok {
			href = strings.TrimSpace(md)
		}
		if href == "" {
			href, _ = link.Attr("href")
			href = strings.TrimSpace(href)
		}
		if href == "" || title == "" || !strings.HasPrefix(href, "http") {
			return
		}
		// 无 data-mdurl 的 so.com/link 包装无法回源，丢弃
		if isSoLinkWrap(href) {
			return
		}

		descSel := sel.Find("p.res-desc")
		if descSel.Length() == 0 {
			descSel = sel.Find("div.res-comm-con")
		}
		content := antirobot.CollapseSpace(strings.TrimSpace(descSel.Text()))

		results = append(results, antirobot.Result{
			Type: antirobot.ResultWeb, Title: title, URL: href,
			Content: content, Engine: "so360",
		})
	})
	return results
}

// isSoLinkWrap 判断是否为 so.com 点击跳转包装链接。
func isSoLinkWrap(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "so.com" || host == "www.so.com") && strings.HasPrefix(u.Path, "/link")
}

// ── 站点屏蔽 ──

func (e *so360Engine) filterBlocked(results []antirobot.Result) []antirobot.Result {
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
