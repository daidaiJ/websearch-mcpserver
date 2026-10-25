// Package sitefetch cleanfetch 站点特判层：对 GitHub / Reddit 这类
// HTML 反爬或 JS 渲染抓不到的站点，按 URL host 归一化改走官方 API / RSS 通道。
// 由 cleanfetch 在本地 webfetch 之前调用（对齐 wikipedia/googlenews「按站点特判」思路）。
package sitefetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
)

// ── GitHub ────────────────────────────────────────────────────────────────────

// apiEndpoint 单测可指向 httptest 服务。
var apiEndpoint = "https://api.github.com"

// GitHubFetcher 通过 GitHub REST API 抓取仓库页：元数据 + README raw。
// 零 Key core 60/h、配 PAT 5000/h（search 之外）；github.com HTML 是 JS 渲染，
// 裸客户端抓不到正文，API 是唯一可靠通道。
type GitHubFetcher struct {
	tokens  []string
	tokenIx atomic.Uint64
	client  *http.Client

	cooldownMu sync.Mutex
	coolUntil  time.Time // 403/429 配额冷却截止（Retry-After / x-ratelimit-reset）
}

// NewGitHubFetcher tokens 为空时零 Key 匿名档（60/h）。
func NewGitHubFetcher(tokens []string) *GitHubFetcher {
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t = strings.TrimSpace(t); t != "" {
			filtered = append(filtered, t)
		}
	}
	return &GitHubFetcher{tokens: filtered, client: &http.Client{Timeout: 15 * time.Second}}
}

// MatchGitHub 判断 URL 是否属于 github.com（特判接管范围，含 www 前缀）。
func MatchGitHub(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return strings.EqualFold(host, "github.com") || strings.EqualFold(host, "www.github.com")
}

// ghRepo API 元数据（仅取用到的字段）。
type ghRepo struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Forks       int    `json:"forks_count"`
	Language    string `json:"language"`
	License     struct {
		SPDX string `json:"spdx_id"`
	} `json:"license"`
	PushedAt      string `json:"pushed_at"`
	DefaultBranch string `json:"default_branch"`
	Homepage      string `json:"homepage"`
}

// Fetch 抓取 github.com 页面并格式化为 Markdown：
//   - /<owner>/<repo>（仓库根）：元数据 + README raw；
//   - /<owner>/<repo>/blob/<branch>/<path>：contents API 取源文件
//     （Accept: application/vnd.github.raw 返回原文；不走 raw.githubusercontent.com——
//     该 CDN 国内无代理出口不可达，2026-10-24 实测）；
//   - 其它路径（issues/pull 等 JS 渲染页）不接管，返回错误由调用方回退常规链路。
func (g *GitHubFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	segs := splitPath(u.Path)
	if len(segs) < 2 {
		return "", errors.New(i18n.T("fetch.sitefetch.github_missing_repo"))
	}
	owner, repo := segs[0], segs[1]
	if len(segs) == 2 {
		return g.fetchRepo(ctx, owner, repo)
	}
	if segs[2] == "blob" && len(segs) >= 5 {
		// blob 页 → contents API 源文件（segs[3]=branch，segs[4:]=文件路径）
		return g.fetchContents(ctx, owner, repo, segs[3], segs[4:])
	}
	return "", errors.New(i18n.T("fetch.sitefetch.github_unsupported_page", "Path", u.Path))
}

// fetchRepo 仓库根：元数据 + README raw 合并为 Markdown。
func (g *GitHubFetcher) fetchRepo(ctx context.Context, owner, repo string) (string, error) {
	var meta ghRepo
	if err := g.apiGet(ctx, fmt.Sprintf("%s/repos/%s/%s", apiEndpoint, owner, repo), &meta); err != nil {
		return "", err
	}
	readme, _ := g.fetchReadme(ctx, owner, repo) // README 缺失不致命（空仓库/纯 issue 仓库）

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", meta.FullName)
	if meta.Description != "" {
		fmt.Fprintf(&sb, "> %s\n\n", strings.TrimSpace(meta.Description))
	}
	var metaBits []string
	if meta.Stars > 0 {
		metaBits = append(metaBits, fmt.Sprintf("★ %d", meta.Stars))
	}
	if meta.Forks > 0 {
		metaBits = append(metaBits, fmt.Sprintf("forks %d", meta.Forks))
	}
	if meta.Language != "" {
		metaBits = append(metaBits, meta.Language)
	}
	if meta.License.SPDX != "" && meta.License.SPDX != "NOASSERTION" {
		metaBits = append(metaBits, meta.License.SPDX)
	}
	if meta.PushedAt != "" {
		metaBits = append(metaBits, i18n.T("fetch.sitefetch.pushed_at", "Date", pushedDate(meta.PushedAt)))
	}
	if len(metaBits) > 0 {
		fmt.Fprintf(&sb, "%s\n\n", strings.Join(metaBits, " · "))
	}
	if meta.Homepage != "" {
		fmt.Fprintf(&sb, "%s\n\n", i18n.T("fetch.sitefetch.homepage", "URL", meta.Homepage))
	}
	if readme != "" {
		sb.WriteString(readme)
	} else {
		sb.WriteString(i18n.T("fetch.sitefetch.readme_missing"))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// pushedDate 取时间戳的 YYYY-MM-DD 段；畸形短串原样返回（裸切片会越界 panic）。
func pushedDate(raw string) string {
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

// fetchReadme 取 README raw（Accept: application/vnd.github.raw 即返回原文而非 base64）。
func (g *GitHubFetcher) fetchReadme(ctx context.Context, owner, repo string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/repos/%s/%s/readme", apiEndpoint, url.PathEscape(owner), url.PathEscape(repo)), nil)
	if err != nil {
		return "", err
	}
	g.setHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.raw")
	resp, err := g.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github readme HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// fetchContents 取 blob 页源文件：contents API + Accept raw 返回原文（消耗 core 配额）。
func (g *GitHubFetcher) fetchContents(ctx context.Context, owner, repo, branch string, filePath []string) (string, error) {
	// path 各段逐段 PathEscape：QueryEscape 会把空格编码成 "+"（query 语义，path 中是
	// 字面加号），裸拼则可能带入未转义的分隔符。
	esc := make([]string, 0, len(filePath))
	for _, seg := range filePath {
		esc = append(esc, url.PathEscape(seg))
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", apiEndpoint, url.PathEscape(owner), url.PathEscape(repo),
			strings.Join(esc, "/"), url.QueryEscape(branch)), nil)
	if err != nil {
		return "", err
	}
	g.setHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.raw")
	resp, err := g.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github contents HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", errors.New(i18n.T("fetch.sitefetch.contents_empty"))
	}
	return text, nil
}

func (g *GitHubFetcher) apiGet(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	g.setHeaders(req)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("github api HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// do 统一执行 API 请求并施加配额冷却：冷却中快速失败（调用方回退常规抓取链），
// 403/429 进入冷却后再失败——匿名 core 60/h、每个仓库页 2 次调用，配额耗尽后
// 连打只会持续 403，主动冷却让 cleanfetch/fetch_top_n 立即走 webfetch→Jina 链。
func (g *GitHubFetcher) do(req *http.Request) (*http.Response, error) {
	if rest := g.cooldownRemaining(); rest > 0 {
		return nil, errors.New(i18n.T("fetch.sitefetch.quota_cooldown", "Seconds", fmt.Sprintf("%.0f", rest.Seconds())))
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		g.enterCooldown(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("github api HTTP %d rate-limited", resp.StatusCode)
	}
	return resp, nil
}

// enterCooldown 按 Retry-After / x-ratelimit-reset 进入冷却（封顶 10 分钟），
// 与搜索引擎侧 github.NewGitHub 同一套口径；本端只做本地冷却，不进全局健康位。
func (g *GitHubFetcher) enterCooldown(resp *http.Response) {
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
	g.cooldownMu.Lock()
	g.coolUntil = until
	g.cooldownMu.Unlock()
	log.WarnT("log.fetch.sitefetch.github_fetch_api_rate_limited_http", "statusCode", resp.StatusCode, "seconds", cd.Seconds())
}

func (g *GitHubFetcher) cooldownRemaining() time.Duration {
	g.cooldownMu.Lock()
	defer g.cooldownMu.Unlock()
	if d := time.Until(g.coolUntil); d > 0 {
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

// setHeaders 公共头：UA 必填（GitHub API 要求），PAT 轮转提额 5000/h。
func (g *GitHubFetcher) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "websearch-mcpserver/1.0 (GitHub fetch)")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if len(g.tokens) > 0 {
		tok := g.tokens[(g.tokenIx.Add(1)-1)%uint64(len(g.tokens))]
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

func splitPath(p string) []string {
	var segs []string
	for _, s := range strings.Split(strings.Trim(p, "/"), "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}
