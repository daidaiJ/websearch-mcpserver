package ddg

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
	"websearch/pkg/i18n"

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

	// 状态代次：每次进入冷却或记录失败递增。请求开始时取快照，成功路径只在
	// 「本次请求期间没有并发请求改写该状态」时清理，避免一次成功清空并发建立的冷却。
	stateEpoch uint64
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
	ddgMaxBackoff      = 90 * time.Second
	ddgSessionLifetime = 25
	ddgResultsPerPage  = 30
)

// 前置退避等待参数改为变量：单测需要缩短以免并发用例被真实 sleep 拖慢
// （与 ddgRateCooldown 同口径，生产取值不变）。
var (
	ddgBaseDelay = 800 * time.Millisecond
	ddgJitter    = 1 * time.Second
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
	return e.SearchContext(context.Background(), query, page, timeRange)
}

// SearchContext 实现 antirobot.ContextEngine：ctx 取消（编排层软超时或客户端断连）
// 后请求立即中止，且本次被判弃的调用不再推进冷却 / 会话轮换等共享状态（B4）。
func (e *ddgEngine) SearchContext(ctx context.Context, query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	start := time.Now()

	if !e.limiter.Allow() {
		return nil, fmt.Errorf("duckduckgo: local rate limit exceeded")
	}

	// 服务端限流冷却期：避让不打上游，快速失败（窗口过后下一次搜索自动恢复）
	if left := e.cooldownRemaining(); left > 0 {
		return nil, fmt.Errorf("duckduckgo: rate-limited by server, cooling down (%.0fs left)", left.Seconds())
	}

	// 状态代次快照：本次请求期间若并发请求进入冷却 / 失败，成功路径不得
	// 顺带清掉它们的避让状态（B2）。
	gen := e.stateGen()

	if err := e.preDelay(ctx); err != nil {
		return nil, err
	}

	status, retryAfter, body, err := e.post(ctx, query, page, timeRange)
	if err != nil {
		e.recordFailIfLive(ctx)
		return nil, err
	}

	// 202 软限流 / 429：预算内等待重试一次，注定超时才放弃进冷却
	if status == http.StatusAccepted || status == http.StatusTooManyRequests {
		return e.handleRateLimited(ctx, start, retryAfter, query, page, timeRange, gen)
	}

	return e.finishOK(ctx, body, gen)
}

// session 本请求使用的会话快照：client 与 UA 在同一次临界区读取，
// 保证一次请求不会混用轮换前后的 client / UA（B1）。
type session struct {
	client *http.Client
	ua     string
}

// takeSession 取会话快照。
func (e *ddgEngine) takeSession() session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return session{client: e.client, ua: e.ua}
}

// stateGen 当前状态代次：请求开始时取快照，成功后据此判断
// 期间是否有并发请求建立过冷却或记录过失败（B2）。
func (e *ddgEngine) stateGen() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateEpoch
}

// finishOK 处理 200 响应：异常页检查 → 解析 → 屏蔽过滤 → 会话轮换。
func (e *ddgEngine) finishOK(ctx context.Context, body []byte, gen uint64) (*antirobot.SearchResponse, error) {
	html := string(body)
	if !strings.Contains(html, "result") {
		e.recordFailIfLive(ctx)
		return nil, fmt.Errorf("no results container found, possibly blocked")
	}

	e.recordSuccessIfLive(ctx, gen)
	results := e.parseResults(html)

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}

	e.rotateSessionIfNeeded()

	return &antirobot.SearchResponse{Engine: "duckduckgo", Results: results}, nil
}

// post 发一次搜索请求，读完 body 后关闭响应体，返回状态码与响应体。
// 只返回状态码而非 *http.Response：响应体在本函数内即被读完并关闭，
// 调用方没有需要保留的响应字段（避免把已关闭 body 的响应交出去）。
func (e *ddgEngine) post(ctx context.Context, query string, page int, timeRange antirobot.TimeRange) (int, time.Duration, []byte, error) {
	req, err := e.buildRequest(query, page, timeRange)
	if err != nil {
		return 0, 0, nil, err
	}
	req = req.WithContext(ctx)
	sess := e.takeSession()
	e.setHeaders(req, sess.ua)

	resp, err := sess.client.Do(req)
	if err != nil {
		return 0, 0, nil, err
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	// Retry-After 必须在关闭响应体前读走。
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return status, retryAfter, nil, err
	}
	if strings.Contains(string(body), "challenge-form") {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err == nil && doc.Find("form#challenge-form").Length() > 0 {
			return status, retryAfter, nil, fmt.Errorf("duckduckgo: CAPTCHA challenge")
		}
	}
	if status != http.StatusOK && status != http.StatusAccepted && status != http.StatusTooManyRequests {
		return status, retryAfter, nil, fmt.Errorf("duckduckgo: HTTP %d", status)
	}
	return status, retryAfter, body, nil
}

// handleRateLimited 限流避让决策：
//   - 等待窗口 + 重试请求能装进本次超时预算 → 等待后重试一次，成功则直接返回；
//   - 注定超时（如默认窗口 20s > 预算 10s）或重试仍限流 → 进入冷却快速失败。
func (e *ddgEngine) handleRateLimited(ctx context.Context, start time.Time, retryAfter time.Duration, query string, page int, timeRange antirobot.TimeRange, gen uint64) (*antirobot.SearchResponse, error) {
	wait := e.cooldownDuration(retryAfter)

	if time.Since(start)+wait+ddgRetryRequestEstimate <= e.searchBudget() {
		if err := sleepCtx(ctx, wait); err != nil {
			return nil, err
		}
		status, _, body, err := e.post(ctx, query, page, timeRange)
		if err != nil {
			e.recordFailIfLive(ctx)
			return nil, err
		}
		if status == http.StatusOK {
			return e.finishOK(ctx, body, gen)
		}
		// 重试仍限流/失败 → 落入冷却放弃
	}

	// 已被放弃的调用不再推进冷却（进入冷却即等于替下一个请求做决定）。
	if ctx.Err() != nil {
		return nil, ctx.Err()
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

func (e *ddgEngine) setHeaders(req *http.Request, ua string) {
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

// preDelay 前置退避等待：可被 ctx 取消（编排层软超时后不再空转）。
func (e *ddgEngine) preDelay(ctx context.Context) error {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := ddgBaseDelay + time.Duration(rand.Int63n(int64(ddgJitter)))
	if bo > 0 {
		delay += bo
	}
	return sleepCtx(ctx, delay)
}

// sleepCtx 按 ctx 可取消地等待 d；d 非正时只回报 ctx 状态。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// recordFailIfLive 只有仍未被放弃的调用才推进退避状态（B4）。
// ctx 已取消说明编排层已按软超时放弃本次调用，其结果不应再影响共享状态。
func (e *ddgEngine) recordFailIfLive(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	e.recordFail()
}

// recordSuccessIfLive 只有仍未被放弃的调用才推进成功状态（B4）。
func (e *ddgEngine) recordSuccessIfLive(ctx context.Context, since uint64) {
	if ctx.Err() != nil {
		return
	}
	e.recordSuccess(since)
}

// recordSuccess 搜索成功：只在「本次请求期间没有并发请求进入冷却或记录失败」时
// 清冷却与退避（B2）。since 为请求开始时的状态代次快照——一次成功只应代表
// 本次请求，不能把并发请求建立的避让状态一并抹掉。
func (e *ddgEngine) recordSuccess(since uint64) {
	e.mu.Lock()
	unchanged := e.stateEpoch == since
	if unchanged {
		e.cooldown = 0
		e.cooldownUntil = time.Time{}
		e.backoff = 0
		e.consecFails = 0
	}
	e.mu.Unlock()
	if unchanged {
		antirobot.ClearCooldown(e.Name())
	}
}

// enterCooldown 进入冷却：时长由 cooldownDuration 计算，冷却期内不打上游。
func (e *ddgEngine) enterCooldown(retryAfter time.Duration) {
	// 档位升级与截止时间在同一临界区内完成读写（B6），避免并发进入冷却时
	// 后写者用旧档位覆盖先写者已升级的冷却。
	e.mu.Lock()
	cd := e.nextCooldownLocked(retryAfter)
	until := time.Now().Add(cd)
	e.cooldown = cd
	e.cooldownUntil = until
	e.stateEpoch++
	e.mu.Unlock()
	antirobot.EnterCooldown(e.Name(), antirobot.HealthKindRateLimit, i18n.T("search.engine.rate_limit_202_429"), until, cd)
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
	defer e.mu.Unlock()
	return antirobot.NextCooldownDuration(e.cooldown, ddgRateCooldown, ddgRateCooldownMax, retryAfter)
}

// nextCooldownLocked 在锁内计算冷却档位（调用方已持锁）：升级规则见
// antirobot.NextCooldownDuration（本项目各引擎共用同一实现）。
func (e *ddgEngine) nextCooldownLocked(retryAfter time.Duration) time.Duration {
	return antirobot.NextCooldownDuration(e.cooldown, ddgRateCooldown, ddgRateCooldownMax, retryAfter)
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
	e.stateEpoch++
	bo := ddgBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > ddgMaxBackoff {
		bo = ddgMaxBackoff
	}
	e.backoff = bo
}

// rotateSessionIfNeeded 会话寿命计数与轮换在同一临界区内完成（B1）：
// 计数读取、判定与状态重写不再跨锁边界，并发请求不会读到轮换中途的半套状态。
func (e *ddgEngine) rotateSessionIfNeeded() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reqCount++
	if e.reqCount >= ddgSessionLifetime {
		e.rotateSessionLocked()
	}
}

// rotateSessionLocked 重建会话（调用方已持锁）：client / ua / 计数一起更新。
func (e *ddgEngine) rotateSessionLocked() {
	e.client = e.opts.newHTTPClient()
	e.ua = ddgUAs[rand.Intn(len(ddgUAs))]
	e.reqCount = 0
}

// rotateSession 供构造期初始化会话（无并发，语义同 rotateSessionLocked）。
func (e *ddgEngine) rotateSession() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rotateSessionLocked()
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
