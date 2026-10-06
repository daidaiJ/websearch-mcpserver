package ddg

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
// DuckDuckGo 通用网页搜索引擎（HTML POST，需代理访问）
// ──────────────────────────────────────────────────────────────────────────────

type ddgEngine struct {
	opts    DuckDuckGoOpts
	limiter *antirobot.RateLimiter

	mu            sync.Mutex
	client        *http.Client
	ua            string
	reqCount      int
	backoff       time.Duration
	consecFails   int
	cooldown      time.Duration // 连续 202 自适应冷却（成功后清零，下次从 ddgRateCooldown 重新开始）
	cooldownUntil time.Time     // 冷却期截止时间，期间不打上游
}

var ddgUAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

var ddgAcceptVariants = []string{
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}

var ddgLangVariants = []string{
	"en-US,en;q=0.9",
	"en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7",
	"zh-CN,zh;q=0.9,en;q=0.8",
}

var ddgTimeRangeMap = map[antirobot.TimeRange]string{
	antirobot.TimeRangeDay:   "d",
	antirobot.TimeRangeWeek:  "w",
	antirobot.TimeRangeMonth: "m",
	antirobot.TimeRangeYear:  "y",
}

// ddgEndpoint 单测可指向 httptest 服务。
var ddgEndpoint = "https://html.duckduckgo.com/html/"

const (
	ddgBaseDelay       = 800 * time.Millisecond
	ddgJitter          = 1 * time.Second
	ddgMaxBackoff      = 90 * time.Second
	ddgSessionLifetime = 25
	ddgResultsPerPage  = 30
)

// 实测（2026-08-30，代理出口）：202 软限流不带 Retry-After 头；202 后 ~11s 仍被限、
// ~17-25s 恢复；触发条件是短窗口内请求密度（1 次成功后 6s 内再打即 202）。
// 避让策略：收到 202/429 先看本次搜索的超时预算——窗口等待 + 重试请求能装进预算
// 就等待后重试一次（本调用仍可成功）；注定超时（如默认窗口 20s > 预算 10s）则
// 进入进程级冷却快速失败，窗口过后下一次搜索自然恢复。
var (
	ddgRateCooldown         = 20 * time.Second // 首次 202 的冷却时长（实测窗口中值）
	ddgRateCooldownMax      = 2 * time.Minute  // 连续 202 的冷却上限
	ddgRetryRequestEstimate = 2 * time.Second  // 重试请求耗时预估（实测 ~1-2s）
)

// ── 接口实现 ──

func (e *ddgEngine) Name() string                    { return "duckduckgo" }
func (e *ddgEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

func (e *ddgEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	start := time.Now()

	if !e.limiter.Allow() {
		return &antirobot.SearchResponse{Engine: "duckduckgo", Results: []antirobot.Result{}}, nil
	}

	// 服务端限流冷却期：避让不打上游，快速失败（窗口过后下一次搜索自动恢复）
	if left := e.cooldownRemaining(); left > 0 {
		return nil, fmt.Errorf("duckduckgo: rate-limited by server, cooling down (%.0fs left)", left.Seconds())
	}

	e.preDelay()

	resp, body, err := e.post(query, page, timeRange)
	if err != nil {
		e.recordFail()
		return nil, err
	}

	// 202 软限流 / 429：预算内等待重试一次，注定超时才放弃进冷却
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return e.handleRateLimited(start, retryAfter, query, page, timeRange)
	}

	return e.finishOK(body)
}

// finishOK 处理 200 响应：异常页检查 → 解析 → 屏蔽过滤 → 会话轮换。
func (e *ddgEngine) finishOK(body []byte) (*antirobot.SearchResponse, error) {
	html := string(body)
	if !strings.Contains(html, "result") {
		e.recordFail()
		return nil, fmt.Errorf("no results container found, possibly blocked")
	}

	e.recordSuccess()
	results := e.parseResults(html)

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}

	e.rotateSessionIfNeeded()

	return &antirobot.SearchResponse{Engine: "duckduckgo", Results: results}, nil
}

// post 发一次搜索请求并读完 body。
func (e *ddgEngine) post(query string, page int, timeRange antirobot.TimeRange) (*http.Response, []byte, error) {
	req, err := e.buildRequest(query, page, timeRange)
	if err != nil {
		return nil, nil, err
	}
	e.setHeaders(req)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	return resp, body, nil
}

// handleRateLimited 限流避让决策：
//   - 等待窗口 + 重试请求能装进本次超时预算 → 等待后重试一次，成功则直接返回；
//   - 注定超时（如默认窗口 20s > 预算 10s）或重试仍限流 → 进入冷却快速失败。
func (e *ddgEngine) handleRateLimited(start time.Time, retryAfter time.Duration, query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	wait := e.cooldownDuration(retryAfter)

	if time.Since(start)+wait+ddgRetryRequestEstimate <= e.searchBudget() {
		time.Sleep(wait)
		resp, body, err := e.post(query, page, timeRange)
		if err == nil && resp.StatusCode == 200 {
			return e.finishOK(body)
		}
		// 重试仍限流/失败 → 落入冷却放弃
	}

	e.enterCooldown(retryAfter)
	e.recordFail()
	return nil, fmt.Errorf("duckduckgo: HTTP 202/429 rate-limited (cooling down %.0fs)", e.cooldownRemaining().Seconds())
}

// searchBudget 单次搜索超时预算（与 Searcher.execOne 的 per-engine 超时对齐）。
func (e *ddgEngine) searchBudget() time.Duration {
	if e.opts.Timeout > 0 {
		return e.opts.Timeout
	}
	return 10 * time.Second
}

// ── 请求构建 ──

func (e *ddgEngine) buildRequest(query string, page int, timeRange antirobot.TimeRange) (*http.Request, error) {
	form := url.Values{}
	form.Set("q", e.applyBlocked(query))
	form.Set("ia", "web")

	// 分页：DDG HTML 版使用 s 参数（0-based offset）
	if page > 1 {
		form.Set("s", fmt.Sprintf("%d", (page-1)*ddgResultsPerPage))
		form.Set("dc", fmt.Sprintf("%d", (page-1)*ddgResultsPerPage))
	}

	// 时间范围
	if tr, ok := ddgTimeRangeMap[timeRange]; ok {
		form.Set("df", tr)
	}

	req, err := http.NewRequest("POST", ddgEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func (e *ddgEngine) setHeaders(req *http.Request) {
	e.mu.Lock()
	ua := e.ua
	e.mu.Unlock()
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", ddgAcceptVariants[rand.Intn(len(ddgAcceptVariants))])
	req.Header.Set("Accept-Language", ddgLangVariants[rand.Intn(len(ddgLangVariants))])
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Referer", "https://html.duckduckgo.com/")
	req.Header.Set("Origin", "https://html.duckduckgo.com")
}

// ── 反爬防御 ──

func (e *ddgEngine) preDelay() {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := ddgBaseDelay + time.Duration(rand.Int63n(int64(ddgJitter)))
	if bo > 0 {
		delay += bo
	}
	time.Sleep(delay)
}

func (e *ddgEngine) recordSuccess() {
	e.mu.Lock()
	e.backoff = 0
	e.consecFails = 0
	e.cooldown = 0
	e.cooldownUntil = time.Time{}
	e.mu.Unlock()
	antirobot.ClearCooldown(e.Name())
}

// enterCooldown 进入冷却：时长由 cooldownDuration 计算，冷却期内不打上游。
func (e *ddgEngine) enterCooldown(retryAfter time.Duration) {
	cd := e.cooldownDuration(retryAfter)
	until := time.Now().Add(cd)
	e.mu.Lock()
	e.cooldown = cd
	e.cooldownUntil = until
	e.mu.Unlock()
	antirobot.EnterCooldown(e.Name(), antirobot.HealthKindRateLimit, "HTTP 202/429 限流", until, cd)
}

// adoptPersistedCooldown 收养上一次进程落盘的冷却：未过期则继续避让至截止时间，
// 已过期仅续接翻倍档位（升级记忆跨进程存活），避免重启后从头试探已被限流的引擎。
func (e *ddgEngine) adoptPersistedCooldown() {
	until, cd, ok := antirobot.AdoptCooldown(e.Name())
	if !ok {
		return
	}
	e.mu.Lock()
	e.cooldown = cd
	if !until.IsZero() {
		e.cooldownUntil = until
	}
	e.mu.Unlock()
}

// cooldownDuration 计算本次冷却时长：连续限流翻倍（上限 ddgRateCooldownMax）；
// 服务端 Retry-After 头明确给出等待时长时以其为准（仍封顶，防异常大值）。
func (e *ddgEngine) cooldownDuration(retryAfter time.Duration) time.Duration {
	e.mu.Lock()
	cd := e.cooldown
	e.mu.Unlock()
	if cd <= 0 {
		cd = ddgRateCooldown
	} else {
		cd *= 2
	}
	if cd > ddgRateCooldownMax {
		cd = ddgRateCooldownMax
	}
	if retryAfter > 0 {
		if retryAfter > ddgRateCooldownMax {
			retryAfter = ddgRateCooldownMax
		}
		cd = retryAfter
	}
	return cd
}

// cooldownRemaining 返回冷却剩余时长，不在冷却期返回 0。
func (e *ddgEngine) cooldownRemaining() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := time.Until(e.cooldownUntil); d > 0 {
		return d
	}
	return 0
}

// parseRetryAfter 解析 Retry-After 头：共享实现见 antirobot.ParseRetryAfter。
func parseRetryAfter(v string) time.Duration {
	return antirobot.ParseRetryAfter(v)
}

func (e *ddgEngine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	bo := ddgBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > ddgMaxBackoff {
		bo = ddgMaxBackoff
	}
	e.backoff = bo
}

func (e *ddgEngine) rotateSessionIfNeeded() {
	e.mu.Lock()
	e.reqCount++
	rotate := e.reqCount >= ddgSessionLifetime
	e.mu.Unlock()
	if rotate {
		e.rotateSession()
	}
}

func (e *ddgEngine) rotateSession() {
	e.client = e.opts.newHTTPClient()
	e.ua = ddgUAs[rand.Intn(len(ddgUAs))]
	e.reqCount = 0
}

// ── HTML 解析 ──

func (e *ddgEngine) parseResults(htmlText string) []antirobot.Result {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}

	var results []antirobot.Result

	doc.Find("div.result").Each(func(_ int, sel *goquery.Selection) {
		// 跳过广告
		if sel.HasClass("result--ad") {
			return
		}

		link := sel.Find("a.result__a").First()
		if link.Length() == 0 {
			return
		}

		title := strings.TrimSpace(link.Text())
		href, _ := link.Attr("href")
		if href == "" || title == "" {
			return
		}

		// DDG 的链接可能是相对路径或跳转链接，提取真实 URL
		href = decodeDDGURL(href)

		// 摘要
		content := ""
		if snippet := sel.Find(".result__snippet").First(); snippet.Length() > 0 {
			content = antirobot.CollapseSpace(strings.TrimSpace(snippet.Text()))
		}

		// 来源
		source := ""
		if src := sel.Find(".result__url").First(); src.Length() > 0 {
			source = strings.TrimSpace(src.Text())
		}

		_ = source // source 可用于未来扩展

		results = append(results, antirobot.Result{
			Type:    antirobot.ResultWeb,
			Title:   title,
			URL:     href,
			Content: content,
			Engine:  "duckduckgo",
		})
	})

	return results
}

// decodeDDGURL 处理 DuckDuckGo 的跳转链接，提取真实 URL。
func decodeDDGURL(href string) string {
	// 处理 //duckduckgo.com/l/?uddg=... 格式
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	if strings.HasSuffix(parsed.Hostname(), "duckduckgo.com") && parsed.Path == "/l/" {
		realURL := parsed.Query().Get("uddg")
		if realURL != "" {
			return realURL
		}
	}
	return href
}

// ── 站点屏蔽 ──

func (e *ddgEngine) applyBlocked(query string) string {
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

func (e *ddgEngine) filterBlocked(results []antirobot.Result) []antirobot.Result {
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
