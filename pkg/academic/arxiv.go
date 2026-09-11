package academic

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
	"websearch/pkg/proxy"
)

// ──────────────────────────────────────────────────────────────────────────────
// arXiv 预印本搜索（国内可直连）
// ──────────────────────────────────────────────────────────────────────────────

// 内置限流约束：arXiv 官方使用条款要求相邻请求 ≥3s（超出返回 429/503），且与 DDG
// 类似存在服务端窗口限流。全局 rate_limit（默认 3/s·60/min）远超容忍度，
// 用户配置比内置上限宽松时钳制到上限，避免触发 429 窗口。
const (
	arxivMaxPerSec   = 1               // 每秒最多 1 次
	arxivMaxPerMin   = 12              // 每分钟最多 12 次（≈5s 平均间隔，官方 3s 间隔之上留安全余量）
	arxivMinInterval = 3 * time.Second // 官方 Tou：1 req/3s
)

// arxivEndpoint 单测可指向 httptest 服务；冷却/预算参数同 DDG 模式，测试可缩短。
var (
	arxivEndpoint      = "https://export.arxiv.org/api/query"
	arxivRateCooldown  = 30 * time.Second // 首次 429 的冷却时长
	arxivCooldownMax   = 2 * time.Minute  // 连续 429 的冷却上限
	arxivRetryEstimate = 2 * time.Second  // 重试请求耗时预估
	arxivSearchBudget  = 10 * time.Second // 单次搜索超时预算（对齐 Searcher.execOne 的 per-engine 超时）
)

type arxivEngine struct {
	client  *http.Client
	limiter *antirobot.RateLimiter

	mu            sync.Mutex
	cooldown      time.Duration
	cooldownUntil time.Time
}

// NewArxiv 创建 arXiv 引擎。client 为 nil 时使用直连默认客户端。
// 限流配置宽松时钳制到内置上限（官方 Tou 1 req/3s + 滑动窗口）。
func NewArxiv(opts antirobot.ArxivOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = proxy.NewDynamicHTTPClient(nil, 15*time.Second)
	}
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = arxivMaxPerSec
	}
	if perMin <= 0 {
		perMin = arxivMaxPerMin
	}
	if perSec > arxivMaxPerSec {
		log.Warnf("arxiv: per_sec=%d 超过内置上限，钳制为 %d（官方 Tou 1 req/3s）", perSec, arxivMaxPerSec)
		perSec = arxivMaxPerSec
	}
	if perMin > arxivMaxPerMin {
		log.Warnf("arxiv: per_min=%d 超过内置上限，钳制为 %d（官方 Tou 1 req/3s ≈ 20/min，取保守值）", perMin, arxivMaxPerMin)
		perMin = arxivMaxPerMin
	}
	return &arxivEngine{
		client:  client,
		limiter: antirobot.NewRateLimiter(perSec, perMin).WithMinInterval(arxivMinInterval),
	}
}

func (e *arxivEngine) Name() string                    { return "arxiv" }
func (e *arxivEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *arxivEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	start := time.Now()

	// 服务端限流冷却期：避让不打上游，快速失败（窗口过后下一次搜索自动恢复）
	if left := e.cooldownRemaining(); left > 0 {
		return nil, fmt.Errorf("arxiv: rate-limited by server, cooling down (%.0fs left)", left.Seconds())
	}

	// 限流等待：官方 Tou ≥3s 间隔 + 滑动窗口；注定超出预算直接放弃，
	// 避免被 execOne 的 per-engine 超时抛弃后仍在后台空转
	for !e.limiter.Allow() {
		if time.Since(start)+arxivRetryEstimate > arxivSearchBudget {
			return nil, fmt.Errorf("arxiv: rate-limit wait would exceed %.0fs budget", arxivSearchBudget.Seconds())
		}
		time.Sleep(500 * time.Millisecond)
	}

	body, status, retryAfter, err := e.fetch(query, page, timeRange)
	if err != nil {
		return nil, err
	}

	// 429/503：预算内等待重试一次，注定超时才放弃进冷却
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return e.handleRateLimited(start, retryAfter, query, page, timeRange)
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("arxiv HTTP %d", status)
	}

	e.recordSuccess()
	return e.parse(body)
}

// fetch 发一次搜索请求并读完 body，返回状态码、Retry-After 解析结果与响应体。
func (e *arxivEngine) fetch(query string, page int, timeRange antirobot.TimeRange) ([]byte, int, time.Duration, error) {
	offset := (page - 1) * 10
	if offset < 0 {
		offset = 0
	}

	q := "all:" + query
	if dateRange := arxivDateRange(timeRange); dateRange != "" {
		q += dateRange
	}

	u := fmt.Sprintf("%s?search_query=%s&start=%d&max_results=10",
		arxivEndpoint, url.QueryEscape(q), offset)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("arxiv request: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("arxiv request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, 0, err
	}
	retryAfter := antirobot.ParseRetryAfter(resp.Header.Get("Retry-After"))
	return body, resp.StatusCode, retryAfter, nil
}

func arxivDateRange(timeRange antirobot.TimeRange) string {
	since := antirobot.TimeRangeSince(timeRange)
	if since == "" {
		return ""
	}
	// arXiv expects YYYYMMDDHHMM and rejects open-ended wildcard ranges.
	start := strings.ReplaceAll(since, "-", "") + "0000"
	end := time.Now().Format("200601021504")
	return " AND submittedDate:[" + start + " TO " + end + "]"
}

// handleRateLimited 限流避让决策：
//   - 等待窗口 + 重试请求能装进本次超时预算 → 等待后重试一次，成功则直接返回；
//   - 注定超时（如默认窗口 30s > 预算 10s）或重试仍限流 → 进入冷却快速失败。
func (e *arxivEngine) handleRateLimited(start time.Time, retryAfter time.Duration, query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	wait := e.cooldownDuration(retryAfter)

	if time.Since(start)+wait+arxivRetryEstimate <= arxivSearchBudget {
		time.Sleep(wait)
		body, status, _, err := e.fetch(query, page, timeRange)
		if err == nil && status == http.StatusOK {
			e.recordSuccess()
			return e.parse(body)
		}
		// 重试仍限流/失败 → 落入冷却放弃
	}

	e.enterCooldown(retryAfter)
	return nil, fmt.Errorf("arxiv: HTTP 429/503 rate-limited (cooling down %.0fs)", e.cooldownRemaining().Seconds())
}

// ── 冷却状态 ──

// cooldownDuration 计算本次冷却时长：连续 429 翻倍自适应（上限 arxivCooldownMax）；
// 服务端 Retry-After 明确给出等待时长时以其为准（仍封顶，防异常大值）。
func (e *arxivEngine) cooldownDuration(retryAfter time.Duration) time.Duration {
	e.mu.Lock()
	cd := e.cooldown
	e.mu.Unlock()
	if cd <= 0 {
		cd = arxivRateCooldown
	} else {
		cd *= 2
	}
	if cd > arxivCooldownMax {
		cd = arxivCooldownMax
	}
	if retryAfter > 0 {
		if retryAfter > arxivCooldownMax {
			retryAfter = arxivCooldownMax
		}
		cd = retryAfter
	}
	return cd
}

// enterCooldown 进入服务端限流冷却，时长由 cooldownDuration 计算。
func (e *arxivEngine) enterCooldown(retryAfter time.Duration) {
	cd := e.cooldownDuration(retryAfter)
	e.mu.Lock()
	e.cooldown = cd
	e.cooldownUntil = time.Now().Add(cd)
	e.mu.Unlock()
}

// cooldownRemaining 返回冷却剩余时长，不在冷却期返回 0。
func (e *arxivEngine) cooldownRemaining() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := time.Until(e.cooldownUntil); d > 0 {
		return d
	}
	return 0
}

// recordSuccess 搜索成功后重置冷却与连续限流计数。
func (e *arxivEngine) recordSuccess() {
	e.mu.Lock()
	e.cooldown = 0
	e.cooldownUntil = time.Time{}
	e.mu.Unlock()
}

// ── XML 解析 ──

type arxivFeed struct {
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	Title     string        `xml:"title"`
	ID        string        `xml:"id"`
	Summary   string        `xml:"summary"`
	Published string        `xml:"published"`
	Authors   []arxivAuthor `xml:"author"`
	Links     []arxivLink   `xml:"link"`
	DOI       string        `xml:"doi"`
	Category  []arxivCat    `xml:"category"`
	Comment   string        `xml:"comment"`
}

type arxivAuthor struct {
	Name string `xml:"name"`
}

type arxivLink struct {
	Href  string `xml:"href,attr"`
	Title string `xml:"title,attr"`
	Type  string `xml:"type,attr"`
}

type arxivCat struct {
	Term string `xml:"term,attr"`
}

func (e *arxivEngine) parse(data []byte) (*antirobot.SearchResponse, error) {
	var feed arxivFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("arxiv parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		if entry.ID == "" {
			continue
		}

		authors := make([]string, 0, len(entry.Authors))
		for _, a := range entry.Authors {
			if a.Name != "" {
				authors = append(authors, a.Name)
			}
		}

		pdfURL := ""
		for _, lnk := range entry.Links {
			if lnk.Title == "pdf" {
				pdfURL = lnk.Href
				break
			}
		}

		title := antirobot.CollapseSpace(strings.TrimSpace(entry.Title))
		summary := antirobot.CollapseSpace(strings.TrimSpace(entry.Summary))

		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         entry.ID,
			Content:     summary,
			PDFURL:      pdfURL,
			Authors:     strings.Join(authors, ", "),
			PublishedAt: entry.Published[:min(len(entry.Published), 10)],
			DOI:         entry.DOI,
			Engine:      "arxiv",
		})
	}

	return &antirobot.SearchResponse{Engine: "arxiv", Results: results}, nil
}
