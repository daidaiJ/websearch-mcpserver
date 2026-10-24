package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"websearch/pkg/search/core"
)

// fakeSearch 可编程的假引擎：记录收到的查询，按 err 决定成败。
type fakeSearch struct {
	name      string
	err       error
	lastQuery string
}

func (f *fakeSearch) Name() string { return f.name }
func (f *fakeSearch) Search(query string) (string, error) {
	f.lastQuery = query
	if f.err != nil {
		return "", f.err
	}
	return "ok", nil
}
func (f *fakeSearch) SearchRaw(query string) ([]core.SearchResult, error) {
	f.lastQuery = query
	if f.err != nil {
		return nil, f.err
	}
	return []core.SearchResult{{Title: "t", Url: "https://example.com", Engine: f.name}}, nil
}
func (f *fakeSearch) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	return f.SearchRaw(query)
}
func (f *fakeSearch) MergeContent(query string, results []core.SearchResult) (string, error) {
	return strings.Join([]string{query, f.name}, "|"), nil
}

func TestGitHubWithFallbackPrimaryWins(t *testing.T) {
	primary := &fakeSearch{name: "github"}
	fallback := &fakeSearch{name: "bing"}
	w := NewGitHubWithFallback(primary, fallback)
	results, err := w.SearchRaw("go scheduler")
	if err != nil {
		t.Fatalf("SearchRaw: %v", err)
	}
	if results[0].Engine != "github" {
		t.Fatalf("主引擎成功应直接返回: %+v", results)
	}
	if fallback.lastQuery != "" {
		t.Fatalf("主引擎成功不应触发回退，但回退收到了 %q", fallback.lastQuery)
	}
}

func TestGitHubWithFallbackTriggersOnPrimaryError(t *testing.T) {
	primary := &fakeSearch{name: "github", err: errors.New("github: HTTP 403 rate-limited")}
	fallback := &fakeSearch{name: "bing"}
	w := NewGitHubWithFallback(primary, fallback)
	results, err := w.SearchRaw("go scheduler")
	if err != nil {
		t.Fatalf("主引擎失败应回退成功: %v", err)
	}
	if results[0].Engine != "bing" {
		t.Fatalf("回退结果应来自 bing: %+v", results)
	}
	if !strings.HasPrefix(fallback.lastQuery, "site:github.com ") {
		t.Fatalf("回退查询应加站点限定符: %q", fallback.lastQuery)
	}
}

func TestSiteScopedQueryNoDuplicate(t *testing.T) {
	if got := siteScopedQuery("SITE:GitHub.com go scheduler"); got != "SITE:GitHub.com go scheduler" {
		t.Fatalf("已有限定符不应重复添加: %q", got)
	}
	if got := siteScopedQuery("go scheduler"); got != "site:github.com go scheduler" {
		t.Fatalf("限定符缺失: %q", got)
	}
}

func TestGitHubWithFallbackCtxCanceled(t *testing.T) {
	primary := &fakeSearch{name: "github", err: context.Canceled}
	fallback := &fakeSearch{name: "bing"}
	w := NewGitHubWithFallback(primary, fallback)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.SearchRawContext(ctx, "q"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx 取消应直接透传，不触发回退: %v", err)
	}
	if fallback.lastQuery != "" {
		t.Fatalf("ctx 取消不应触发回退: %q", fallback.lastQuery)
	}
}

func TestGitHubWithFallbackNilFallback(t *testing.T) {
	primary := &fakeSearch{name: "github", err: errors.New("rate-limited")}
	w := NewGitHubWithFallback(primary, nil)
	if _, err := w.SearchRaw("q"); err == nil {
		t.Fatal("无回退引擎时应透传主引擎错误")
	}
}
