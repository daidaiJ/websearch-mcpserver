package baidu

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"websearch/pkg/antirobot"
)

// ──────────────────────────────────────────────────────────────────────────────
// 百度网页搜索引擎（tn=json JSON API，无需 API Key）
// 参考: https://github.com/searxng/searxng/blob/master/searx/engines/baidu.py
// ──────────────────────────────────────────────────────────────────────────────

type baiduEngine struct {
	opts    BaiduOpts
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

var baiduUAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
}

var baiduAcceptVariants = []string{
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
	"text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
}

var baiduLangVariants = []string{
	"zh-CN,zh;q=0.9,en;q=0.8",
	"zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
}

const (
	baiduMaxBackoff      = 60 * time.Second
	baiduSessionLifetime = 30
	baiduResultsPerPage  = 10
)

// 前置退避等待参数改为变量：单测需要缩短（生产取值不变）。
var (
	baiduBaseDelay = 500 * time.Millisecond
	baiduJitter    = 800 * time.Millisecond
)

// ── JSON 响应结构 ──

type baiduFeedEntry struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Abs   string `json:"abs"`
	Time  int64  `json:"time"`
}

type baiduFeed struct {
	Entry []baiduFeedEntry `json:"entry"`
}

type baiduResponse struct {
	Feed baiduFeed `json:"feed"`
}

// ── 接口实现 ──

func (e *baiduEngine) Name() string                    { return "baidu_web" }
func (e *baiduEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *baiduEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	if !e.limiter.Allow() {
		// 本端限流：返回哨兵错误而不是「空结果 + nil」（B12）
		return antirobot.LocalLimitSkipped("baidu_web")
	}

	gen := e.stateGen()

	e.preDelay()
	u := e.buildURL(query, page, timeRange)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	sess := e.takeSession()
	e.setHeaders(req, sess.ua)

	resp, err := sess.client.Do(req)
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

	if resp.StatusCode == 429 || resp.StatusCode == 403 {
		e.recordFail()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		e.recordFail()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 检测百度验证码重定向
	if strings.Contains(resp.Request.URL.String(), "wappass.baidu.com/static/captcha") {
		e.recordFail()
		return nil, fmt.Errorf("blocked by Baidu CAPTCHA")
	}

	results, err := e.parseResponse(body)
	if err != nil {
		e.recordFail()
		return nil, err
	}

	e.recordSuccess(gen)

	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}

	e.rotateSessionIfNeeded()

	return &antirobot.SearchResponse{Engine: "baidu_web", Results: results}, nil
}

// session 本请求使用的会话快照：client 与 UA 在同一次临界区读取，
// 保证一次请求不会混用轮换前后的 client / UA（B1）。
type session struct {
	client *http.Client
	ua     string
}

// takeSession 取会话快照。
func (e *baiduEngine) takeSession() session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return session{client: e.client, ua: e.ua}
}

// stateGen 当前状态代次（请求开始时取快照，成功后据此判断期间是否有并发失败）。
func (e *baiduEngine) stateGen() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateEpoch
}

// ── URL 构造 ──

func (e *baiduEngine) buildURL(query string, page int, timeRange antirobot.TimeRange) string {
	q := url.Values{}
	q.Set("wd", e.applyBlocked(query))
	q.Set("rn", strconv.Itoa(baiduResultsPerPage))
	q.Set("pn", strconv.Itoa((page-1)*baiduResultsPerPage))
	q.Set("tn", "json")

	if seconds := baiduTimeRangeSeconds(timeRange); seconds > 0 {
		now := int(time.Now().Unix())
		past := now - seconds
		q.Set("gpc", fmt.Sprintf("stf=%d,%d|stftype=1", past, now))
	}

	return "https://www.baidu.com/s?" + q.Encode()
}

func baiduTimeRangeSeconds(tr antirobot.TimeRange) int {
	switch tr {
	case antirobot.TimeRangeDay:
		return 86400
	case antirobot.TimeRangeWeek:
		return 604800
	case antirobot.TimeRangeMonth:
		return 2592000
	case antirobot.TimeRangeYear:
		return 31536000
	default:
		return 0
	}
}

// ── 请求头 ──

func (e *baiduEngine) setHeaders(req *http.Request, ua string) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", baiduAcceptVariants[rand.Intn(len(baiduAcceptVariants))])
	req.Header.Set("Accept-Language", baiduLangVariants[rand.Intn(len(baiduLangVariants))])
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Referer", "https://www.baidu.com/")
}

// ── 反爬防御 ──

func (e *baiduEngine) preDelay() {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := baiduBaseDelay + time.Duration(rand.Int63n(int64(baiduJitter)))
	if bo > 0 {
		delay += bo
	}
	time.Sleep(delay)
}

// recordSuccess 搜索成功：只在「本次请求期间没有并发请求记录失败」时清退避（B2）。
func (e *baiduEngine) recordSuccess(since uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stateEpoch != since {
		return
	}
	e.backoff = 0
	e.consecFails = 0
}

func (e *baiduEngine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	e.stateEpoch++
	bo := baiduBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > baiduMaxBackoff {
		bo = baiduMaxBackoff
	}
	e.backoff = bo
}

// rotateSessionIfNeeded 会话寿命计数与轮换在同一临界区内完成（B1）。
func (e *baiduEngine) rotateSessionIfNeeded() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reqCount++
	if e.reqCount >= baiduSessionLifetime {
		e.rotateSessionLocked()
	}
}

// rotateSessionLocked 重建会话（调用方已持锁）：client / ua / 计数一起更新。
func (e *baiduEngine) rotateSessionLocked() {
	e.client = &http.Client{Timeout: 15 * time.Second}
	e.ua = baiduUAs[rand.Intn(len(baiduUAs))]
	e.reqCount = 0
}

// rotateSession 构造期初始化会话（无并发）。
func (e *baiduEngine) rotateSession() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rotateSessionLocked()
}

// ── 响应解析 ──

func (e *baiduEngine) parseResponse(body []byte) ([]antirobot.Result, error) {
	var resp baiduResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	if len(resp.Feed.Entry) == 0 {
		return nil, fmt.Errorf("empty response: no entries in feed")
	}

	results := make([]antirobot.Result, 0, len(resp.Feed.Entry))
	for _, entry := range resp.Feed.Entry {
		if entry.Title == "" || entry.URL == "" {
			continue
		}

		title := html.UnescapeString(entry.Title)
		content := html.UnescapeString(entry.Abs)

		publishedAt := ""
		dateSource := ""
		if entry.Time > 0 {
			publishedAt = time.Unix(entry.Time, 0).Format("2006-01-02")
			dateSource = antirobot.DateSourceStructured
		}

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultWeb,
			Title:       title,
			URL:         entry.URL,
			Content:     content,
			PublishedAt: publishedAt,
			DateSource:  dateSource,
			Engine:      "baidu_web",
		})
	}

	return results, nil
}

// ── 站点屏蔽 ──

func (e *baiduEngine) applyBlocked(query string) string {
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

func (e *baiduEngine) filterBlocked(results []antirobot.Result) []antirobot.Result {
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
