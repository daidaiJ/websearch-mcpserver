// Package github GitHub 搜索引擎——REST API（零 Key 默认可用，PAT 提额）。
// 国内出口直连可达（2026-10-24 实测，无需代理，与 wikipedia/googlenews 的代理门槛不同）。
// 匿名 search 10/min，配 PAT 提额 30/min（官方 docs 核对）；code search 要求认证，
// 本引擎仅用 repositories 搜索。github.com HTML 搜索是 JS 渲染抓不到，API 是唯一正道。
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
)

// searchEndpoint 单测可指向 httptest 服务。
var searchEndpoint = "https://api.github.com/search/repositories"

// apiVersion 官方推荐的 API 版本头。
const apiVersion = "2022-11-28"

// 匿名 / 认证档的 search 限流（官方：匿名 10/min，PAT 30/min）。
const (
	anonPerMin    = 10
	authedPerMin  = 30
	defaultNum    = 10
	maxNum        = 50
	maxQueryLen   = 256
	searchClauses = 5 // GitHub search API 查询子句上限，超长查询按空格截断保护
)

// GitHubOpts GitHub 引擎配置。
type GitHubOpts struct {
	Enabled    bool
	Tokens     []string // PAT 列表（轮转提额）；空 = 匿名档
	NumResults int      // 单次结果数（默认 10，上限 50）
	PerMin     int      // 每分钟限流（默认按是否配 token 取 10/30）
}

// NewGitHub 创建 GitHub 搜索引擎（直连可达，不要求代理）。
func NewGitHub(opts GitHubOpts) antirobot.Engine {
	num := opts.NumResults
	if num <= 0 {
		num = defaultNum
	}
	if num > maxNum {
		num = maxNum
	}
	perMin := opts.PerMin
	if perMin <= 0 {
		if len(opts.Tokens) > 0 {
			perMin = authedPerMin
		} else {
			perMin = anonPerMin
		}
	}
	tokens := make([]string, 0, len(opts.Tokens))
	seen := make(map[string]struct{}, len(opts.Tokens))
	for _, t := range opts.Tokens {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue // 同 KeyPool 口径：重复 Key 去重，避免轮转空转
		}
		seen[t] = struct{}{}
		tokens = append(tokens, t)
	}
	return &githubEngine{
		opts:    opts,
		num:     num,
		tokens:  tokens,
		limiter: antirobot.NewRateLimiter(1, perMin),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type githubEngine struct {
	opts    GitHubOpts
	num     int
	tokens  []string
	tokenIx atomic.Uint64
	limiter *antirobot.RateLimiter
	client  *http.Client

	cooldownMu sync.Mutex
	coolUntil  time.Time // 429/403 限流冷却截止（Retry-After 或 x-ratelimit-reset）
}

func (e *githubEngine) Name() string                    { return "github" }
func (e *githubEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

// ghRepo GitHub search API 的仓库条目（仅取用到的字段）。
type ghRepo struct {
	FullName    string `json:"full_name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`
	CreatedAt   string `json:"created_at"`
	PushedAt    string `json:"pushed_at"`
}

type ghSearchResp struct {
	Items []ghRepo `json:"items"`
}

func (e *githubEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	if rest := e.cooldownRemaining(); rest > 0 {
		return nil, fmt.Errorf("github: HTTP 403/429 rate-limited (cooling down %.0fs)", rest.Seconds())
	}
	if !e.limiter.Allow() {
		return &antirobot.SearchResponse{Engine: "github", Results: []antirobot.Result{}}, nil
	}

	u := e.buildURL(query, page, timeRange)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch-mcpserver/1.0 (GitHub search engine)")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if tok := e.nextToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	// GitHub 限流形态：403（配额耗尽，headers 带提示）与 429（二级限流，Retry-After）。
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		e.enterCooldown(resp)
		return nil, fmt.Errorf("github: HTTP %d rate-limited", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var parsed ghSearchResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("github API 响应解析失败: %w", err)
	}
	results := make([]antirobot.Result, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		fullName := strings.TrimSpace(item.FullName)
		if fullName == "" || strings.TrimSpace(item.HTMLURL) == "" {
			continue
		}
		content := strings.TrimSpace(item.Description)
		var meta []string
		if item.Stars > 0 {
			meta = append(meta, fmt.Sprintf("★%d", item.Stars))
		}
		if lang := strings.TrimSpace(item.Language); lang != "" {
			meta = append(meta, lang)
		}
		if len(meta) > 0 {
			content = strings.TrimSpace(content + " (" + strings.Join(meta, " · ") + ")")
		}
		r := antirobot.Result{
			Type:    antirobot.ResultWeb,
			Title:   fullName,
			URL:     strings.TrimSpace(item.HTMLURL),
			Content: content,
			Engine:  "github",
		}
		if t, ok := parseAPITime(item.CreatedAt); ok {
			r.PublishedAt = t.Format("2006-01-02")
			r.DateSource = antirobot.DateSourceStructured // created_at 为仓库创建时间（结构化字段）
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		log.Debugf("github: %q 无结果", query)
	}
	return &antirobot.SearchResponse{Engine: "github", Results: results}, nil
}

// buildURL 构造 repository 搜索 URL；freshness 映射为 created:> 限定符，
// 翻页用 page 参数（per_page=num）。
func (e *githubEngine) buildURL(query string, page int, timeRange antirobot.TimeRange) string {
	// GitHub 查询是 URL 子句语法，调用方拼入的 site:/-site: 等操作符子句天然兼容；
	// 仅防超长查询打爆 URL。
	if len(query) > maxQueryLen {
		query = truncateClauses(query)
	}
	if since := antirobot.TimeRangeSince(timeRange); since != "" {
		query = query + " created:>" + since
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("per_page", fmt.Sprintf("%d", e.num))
	if page > 1 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	return searchEndpoint + "?" + q.Encode()
}

// truncateClauses 保留前 searchClauses 个空格分隔子句（引号短语视为一个子句）。
func truncateClauses(query string) string {
	fields := strings.Fields(query)
	if len(fields) <= searchClauses {
		return query
	}
	return strings.Join(fields[:searchClauses], " ")
}

// nextToken 轮转返回下一个 PAT（无 token 返回空 = 匿名档）。
func (e *githubEngine) nextToken() string {
	if len(e.tokens) == 0 {
		return ""
	}
	return e.tokens[(e.tokenIx.Add(1)-1)%uint64(len(e.tokens))]
}

// enterCooldown 按 Retry-After / x-ratelimit-reset 进入冷却（封顶 10 分钟）。
func (e *githubEngine) enterCooldown(resp *http.Response) {
	cd := antirobot.ParseRetryAfter(resp.Header.Get("Retry-After"))
	if cd <= 0 {
		if reset := parseResetHeader(resp.Header.Get("x-ratelimit-reset")); !reset.IsZero() {
			cd = time.Until(reset)
		}
	}
	if cd <= 0 {
		cd = 60 * time.Second
	}
	if cd > 10*time.Minute {
		cd = 10 * time.Minute
	}
	until := time.Now().Add(cd)
	e.cooldownMu.Lock()
	e.coolUntil = until
	e.cooldownMu.Unlock()
	antirobot.EnterCooldown(e.Name(), antirobot.HealthKindRateLimit, "HTTP 403/429 限流", until, cd)
}

func (e *githubEngine) cooldownRemaining() time.Duration {
	e.cooldownMu.Lock()
	defer e.cooldownMu.Unlock()
	if d := time.Until(e.coolUntil); d > 0 {
		return d
	}
	return 0
}

// parseResetHeader 解析 x-ratelimit-reset（epoch 秒）。
func parseResetHeader(v string) time.Time {
	var sec int64
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &sec); err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// parseAPITime 解析 GitHub API 的 RFC3339 时间戳。
func parseAPITime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
