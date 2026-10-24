package adapter

import (
	"context"
	"strings"

	"websearch/pkg/search/core"
)

// ──────────────────────────────────────────────────────────────────────────────
// GitHubWithFallback GitHub API 失败时自动回退 Bing site:github.com 检索
// ──────────────────────────────────────────────────────────────────────────────

// GitHubWithFallback 包装 GitHub REST API 引擎：API 失败（限流冷却 / 403 /
// 网络错误 / 零结果）时回退 Bing 并自动加 site:github.com 限定符，
// 保证 GitHub 相关检索仍有产出（对齐 BaiduWithFallback 的回退语义）。
type GitHubWithFallback struct {
	primary  core.SearchInf // GitHub REST API 引擎
	fallback core.SearchInf // Bing 适配器（共享全局 fallback 实例，查询时加限定符）
}

// NewGitHubWithFallback 创建带回退的 GitHub 搜索引擎；fallback 为 nil 时退化为直通。
func NewGitHubWithFallback(primary, fallback core.SearchInf) *GitHubWithFallback {
	return &GitHubWithFallback{primary: primary, fallback: fallback}
}

func (b *GitHubWithFallback) Name() string { return "github" }

func (b *GitHubWithFallback) Search(query string) (string, error) {
	results, err := b.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return b.MergeContent(query, results)
}

func (b *GitHubWithFallback) SearchRaw(query string) ([]core.SearchResult, error) {
	return b.SearchRawContext(context.Background(), query)
}

// SearchRawContext 实现 core.ContextSearcher：ctx 透传给主引擎与回退引擎。
func (b *GitHubWithFallback) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	results, err := searchWithContext(ctx, b.primary, query)
	if err == nil {
		return results, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err() // 请求已取消/超时，不再触发回退
	}
	if b.fallback == nil {
		return nil, err
	}
	// API 失败，回退 Bing 并加站点限定符
	return searchWithContext(ctx, b.fallback, siteScopedQuery(query))
}

// siteScopedQuery 给查询加 site:github.com 限定符（去重已有同名限定符）。
func siteScopedQuery(query string) string {
	const qualifier = "site:github.com"
	for _, tok := range strings.Fields(query) {
		if strings.EqualFold(tok, qualifier) {
			return query
		}
	}
	return qualifier + " " + query
}

func (b *GitHubWithFallback) MergeContent(query string, results []core.SearchResult) (string, error) {
	return b.primary.MergeContent(query, results)
}
