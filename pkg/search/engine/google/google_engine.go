package google

import (
	"context"
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
// Google 通用网页搜索引擎（HTML 解析，需代理访问）
// 参考: https://github.com/searxng/searxng/blob/master/searx/engines/google.py
// ──────────────────────────────────────────────────────────────────────────────

type googleEngine struct {
	opts    GoogleOpts
	limiter *antirobot.RateLimiter

	mu          sync.Mutex
	client      *http.Client
	ua          string
	reqCount    int
	backoff     time.Duration
	consecFails int

	// 状态代次：每次记录失败递增。请求开始时取快照，成功路径只在
	// 「本次请求期间没有并发请求记录失败」时清退避（B2）。
	stateEpoch uint64
}

var googleUAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

var googleAcceptVariants = []string{
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
}

var googleLangVariants = []string{
	"en-US,en;q=0.9",
	"en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7",
	"zh-CN,zh;q=0.9,en;q=0.8",
}

var timeRangeMap = map[antirobot.TimeRange]string{
	antirobot.TimeRangeDay:   "d",
	antirobot.TimeRangeWeek:  "w",
	antirobot.TimeRangeMonth: "m",
	antirobot.TimeRangeYear:  "y",
}

const (
	googleMaxBackoff      = 120 * time.Second
	googleSessionLifetime = 20
	googleResultsPerPage  = 10
)

// 前置退避等待参数改为变量：单测需要缩短（生产取值不变）。
var (
	googleBaseDelay = 1 * time.Second
	googleJitter    = 1 * time.Second
)

// ── 接口实现 ──

func (e *googleEngine) Name() string                    { return "google" }
func (e *googleEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

func (e *googleEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	return e.SearchContext(context.Background(), query, page, timeRange)
}

// SearchContext 实现 antirobot.ContextEngine：ctx 取消后请求立即中止，
// 且被放弃的调用不再推进退避 / 会话轮换（B4）。
func (e *googleEngine) SearchContext(ctx context.Context, query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !e.limiter.Allow() {
		// 本端限流：返回哨兵错误而不是「空结果 + nil」（B12）
		return antirobot.LocalLimitSkipped("google")
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
	sess := e.takeSession()
	e.setHeaders(req, sess.ua)
	// 绕过 Cookie 同意页面
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+"})

	resp, err := sess.client.Do(req)
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

	// 检测 Google CAPTCHA / sorry 页面
	if err := detectSorry(resp, string(body)); err != nil {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, err
	}

	if resp.StatusCode != 200 {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	results := e.parseResults(string(body))
	if len(results) == 0 {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, fmt.Errorf("no results parsed (possible anti-bot page, URL: %s)", resp.Request.URL)
	}

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}

	if ctx.Err() == nil {
		e.recordSuccess(gen)
		e.rotateSessionIfNeeded()
	}

	return &antirobot.SearchResponse{Engine: "google", Results: results}, nil
}

var _ antirobot.ContextEngine = (*googleEngine)(nil)

// session 本请求使用的会话快照（B1）。
type session struct {
	client *http.Client
	ua     string
}

// takeSession 取会话快照。
func (e *googleEngine) takeSession() session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return session{client: e.client, ua: e.ua}
}

// stateGen 当前状态代次。
func (e *googleEngine) stateGen() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateEpoch
}

// ── CAPTCHA / 安全挑战检测 ──

func detectSorry(resp *http.Response, body string) error {
	if resp.Request != nil {
		host := resp.Request.URL.Hostname()
		if host == "sorry.google.com" || strings.Contains(resp.Request.URL.Path, "/sorry") {
			return fmt.Errorf("blocked by Google CAPTCHA (sorry page)")
		}
	}
	if resp.StatusCode == 302 {
		return fmt.Errorf("blocked by Google CAPTCHA (302 redirect)")
	}
	if len(body) < 2000 && strings.Contains(body, "/sorry/") {
		return fmt.Errorf("blocked by Google CAPTCHA (sorry page in body)")
	}
	// 检测 Google JS Challenge 页面（安全验证，非搜索结果）
	if strings.Contains(body, "/httpservice/retry/enablejs") ||
		strings.Contains(body, "SG_SS") && !strings.Contains(body, `id="rso"`) {
		return fmt.Errorf("blocked by Google Security Challenge (JS challenge page)")
	}
	return nil
}

// ── URL 构造 ──

func (e *googleEngine) buildURL(query string, page int, timeRange antirobot.TimeRange) string {
	q := url.Values{}
	q.Set("q", e.applyBlocked(query))
	q.Set("hl", "en")
	q.Set("ie", "utf8")
	q.Set("oe", "utf8")
	q.Set("filter", "0")

	start := (page - 1) * googleResultsPerPage
	if start > 0 {
		q.Set("start", fmt.Sprintf("%d", start))
	}

	if tr, ok := timeRangeMap[timeRange]; ok {
		q.Set("tbs", "qdr:"+tr)
	}

	return "https://www.google.com/search?" + q.Encode()
}

// ── 请求头 ──

func (e *googleEngine) setHeaders(req *http.Request, ua string) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", googleAcceptVariants[rand.Intn(len(googleAcceptVariants))])
	req.Header.Set("Accept-Language", googleLangVariants[rand.Intn(len(googleLangVariants))])
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
}

// ── 反爬防御 ──

func (e *googleEngine) preDelay(ctx context.Context) error {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := googleBaseDelay + time.Duration(rand.Int63n(int64(googleJitter)))
	if bo > 0 {
		delay += bo
	}
	return antirobot.SleepCtx(ctx, delay)
}

// recordSuccess 搜索成功：只在「本次请求期间没有并发请求记录失败」时清退避（B2）。
func (e *googleEngine) recordSuccess(since uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stateEpoch != since {
		return
	}
	e.backoff = 0
	e.consecFails = 0
}

func (e *googleEngine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	e.stateEpoch++
	bo := googleBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > googleMaxBackoff {
		bo = googleMaxBackoff
	}
	e.backoff = bo
}

// rotateSessionIfNeeded 会话寿命计数与轮换在同一临界区内完成（B1）。
func (e *googleEngine) rotateSessionIfNeeded() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reqCount++
	if e.reqCount >= googleSessionLifetime {
		e.rotateSessionLocked()
	}
}

// rotateSessionLocked 重建会话（调用方已持锁）。
func (e *googleEngine) rotateSessionLocked() {
	e.client = e.opts.newHTTPClient()
	e.ua = googleUAs[rand.Intn(len(googleUAs))]
	e.reqCount = 0
}

// rotateSession 构造期初始化会话（无并发）。
func (e *googleEngine) rotateSession() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rotateSessionLocked()
}

// ── HTML 解析 ──

func (e *googleEngine) parseResults(htmlText string) []antirobot.Result {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}

	// 防御性检查：确认页面包含搜索结果容器
	if doc.Find("div#rso").Length() == 0 && doc.Find("div#search").Length() == 0 {
		return nil
	}

	// 策略一：SearXNG 方式 — a[data-ved]:not([class]) 作为入口
	if results := e.parseByDataVed(doc); len(results) > 0 {
		return results
	}

	// 策略二：传统方式 — div.g 作为入口
	return e.parseByDivG(doc)
}

// parseByDataVed 参考 SearXNG，使用 a[data-ved]:not([class]) 作为结果入口。
// 这种方式不依赖特定的容器 class，更稳定。
func (e *googleEngine) parseByDataVed(doc *goquery.Document) []antirobot.Result {
	var results []antirobot.Result
	seen := make(map[string]bool)

	doc.Find("a[data-ved]:not([class])").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if href == "" {
			return
		}

		// 过滤掉非搜索结果链接（导航、页脚等）
		if !isSearchResultLink(href) {
			return
		}

		// 提取真实 URL
		realURL := cleanGoogleURL(href)
		if realURL == "" || seen[realURL] {
			return
		}

		// 提取标题：链接内带 style 属性的 div，或 h3
		title := ""
		if titleDiv := a.Find("div[style]").First(); titleDiv.Length() > 0 {
			title = strings.TrimSpace(titleDiv.Text())
		}
		if title == "" {
			title = strings.TrimSpace(a.Find("h3").First().Text())
		}
		if title == "" {
			return
		}

		// 提取摘要：从链接向上找到结果容器，在其中查找摘要
		content := extractContentFromAncestor(a)

		seen[realURL] = true
		results = append(results, antirobot.Result{
			Type:    antirobot.ResultWeb,
			Title:   title,
			URL:     realURL,
			Content: antirobot.CollapseSpace(content),
			Engine:  "google",
		})
	})

	return results
}

// parseByDivG 传统方式，使用 div.g 作为结果容器。
func (e *googleEngine) parseByDivG(doc *goquery.Document) []antirobot.Result {
	var results []antirobot.Result

	doc.Find("div.g").Each(func(_ int, sel *goquery.Selection) {
		titleSel := sel.Find("h3").First()
		if titleSel.Length() == 0 {
			return
		}
		title := strings.TrimSpace(titleSel.Text())
		if title == "" {
			return
		}

		href := extractURL(sel)
		if href == "" {
			return
		}

		content := extractContent(sel)

		results = append(results, antirobot.Result{
			Type:    antirobot.ResultWeb,
			Title:   title,
			URL:     href,
			Content: antirobot.CollapseSpace(content),
			Engine:  "google",
		})
	})

	return results
}

// isSearchResultLink 判断链接是否为搜索结果（非导航/页脚等）。
func isSearchResultLink(href string) bool {
	if strings.HasPrefix(href, "/url?q=") {
		return true
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		// 排除 Google 自身的导航链接
		if strings.Contains(href, "google.com/search") ||
			strings.Contains(href, "accounts.google") ||
			strings.Contains(href, "support.google") ||
			strings.Contains(href, "policies.google") {
			return false
		}
		return true
	}
	return false
}

// cleanGoogleURL 清理 Google 重定向链接，提取真实 URL。
func cleanGoogleURL(href string) string {
	if strings.HasPrefix(href, "/url?q=") {
		u, err := url.Parse(href)
		if err == nil {
			q := u.Query().Get("q")
			if q != "" {
				return q
			}
		}
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return ""
}

// extractContentFromAncestor 从链接的祖先元素中提取摘要文本。
// 参考 SearXNG 的 ../..//div[contains(@class, "...")] 逻辑。
func extractContentFromAncestor(a *goquery.Selection) string {
	// 向上遍历 3~5 层祖先，查找包含摘要的 div
	parent := a.Parent()
	for i := 0; i < 5; i++ {
		if parent.Length() == 0 {
			break
		}
		// 在当前祖先中查找已知的摘要选择器
		for _, selector := range []string{
			"div.VwiC3b",
			"div[data-sncf]",
			"div.IsZvec",
			"span.aCOpRe",
			"div[style*='line-clamp']",
		} {
			if s := parent.Find(selector).First(); s.Length() > 0 {
				text := strings.TrimSpace(s.Text())
				if text != "" && len(text) > 20 {
					return text
				}
			}
		}
		parent = parent.Parent()
	}
	return ""
}

// extractURL 从 div.g 容器中提取链接（传统方式使用）。
func extractURL(sel *goquery.Selection) string {
	var href string
	sel.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if href != "" {
			return
		}
		h, _ := a.Attr("href")
		if strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
			href = h
		}
	})
	if href != "" {
		return href
	}
	sel.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if href != "" {
			return
		}
		h, _ := a.Attr("href")
		if strings.HasPrefix(h, "/url?q=") {
			u, err := url.Parse(h)
			if err == nil {
				q := u.Query().Get("q")
				if q != "" {
					href = q
				}
			}
		}
	})
	return href
}

// extractContent 从 div.g 容器中提取摘要（传统方式使用）。
func extractContent(sel *goquery.Selection) string {
	for _, selector := range []string{
		"div[data-sncf]",
		"div.VwiC3b",
		"div.IsZvec",
		"span.aCOpRe",
	} {
		if s := sel.Find(selector).First(); s.Length() > 0 {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				return text
			}
		}
	}
	fullText := strings.TrimSpace(sel.Text())
	titleText := strings.TrimSpace(sel.Find("h3").First().Text())
	return strings.TrimSpace(strings.Replace(fullText, titleText, "", 1))
}

// ── 站点屏蔽 ──

func (e *googleEngine) applyBlocked(query string) string {
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

func (e *googleEngine) filterBlocked(results []antirobot.Result) []antirobot.Result {
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
