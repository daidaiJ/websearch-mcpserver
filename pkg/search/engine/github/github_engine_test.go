package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"websearch/pkg/antirobot"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// stubServer 起一个按 handler 响应的 httptest 并替换 searchEndpoint。
func stubServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	old := searchEndpoint
	searchEndpoint = srv.URL
	t.Cleanup(func() { searchEndpoint = old })
	return srv
}

func TestGitHubSearchMapsResults(t *testing.T) {
	var gotAuth, gotUA, gotAccept string
	var gotQuery string
	stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"full_name":        "golang/go",
					"html_url":         "https://github.com/golang/go",
					"description":      "The Go programming language",
					"stargazers_count": 125000,
					"language":         "Go",
					"created_at":       "2007-11-10T00:00:00Z",
				},
				{"full_name": "", "html_url": "https://github.com/empty/empty"},
			},
		})
	})

	eng := NewGitHub(GitHubOpts{Enabled: true, NumResults: 10})
	resp, err := eng.Search("go scheduler", 1, antirobot.TimeRangeNone)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("期望 1 条结果（空 full_name 应跳过），got %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Title != "golang/go" || r.URL != "https://github.com/golang/go" || r.Engine != "github" {
		t.Fatalf("结果字段不符: %+v", r)
	}
	if r.Content == "" || !contains(r.Content, "★125000") || !contains(r.Content, "Go") {
		t.Fatalf("内容应含描述与 ★/语言元信息: %q", r.Content)
	}
	if r.PublishedAt != "2007-11-10" || r.DateSource != antirobot.DateSourceStructured {
		t.Fatalf("created_at 应映射为结构化日期: %q/%q", r.PublishedAt, r.DateSource)
	}
	if gotAuth != "" {
		t.Fatalf("匿名档不应带 Authorization: %q", gotAuth)
	}
	if !contains(gotUA, "websearch-mcpserver") {
		t.Fatalf("UA 缺失: %q", gotUA)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Fatalf("Accept 头不符: %q", gotAccept)
	}
	if gotQuery != "go scheduler" {
		t.Fatalf("查询透传不符: %q", gotQuery)
	}
}

func TestGitHubSearchTokenRotation(t *testing.T) {
	var auths []string
	stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	eng := NewGitHub(GitHubOpts{Enabled: true, Tokens: []string{"tok-a", "tok-b", "", "tok-b"}})
	eng.(*githubEngine).limiter = antirobot.NewRateLimiter(1000, 1000) // 绕过 1/s 钳制便于连发
	// 2 个有效 token（空串剔除、重复去重）轮转：tok-a → tok-b → tok-a
	for i := 0; i < 3; i++ {
		if _, err := eng.Search("q", 1, antirobot.TimeRangeNone); err != nil {
			t.Fatalf("Search#%d: %v", i, err)
		}
	}
	want := []string{"Bearer tok-a", "Bearer tok-b", "Bearer tok-a"}
	if len(auths) != 3 || auths[0] != want[0] || auths[1] != want[1] || auths[2] != want[2] {
		t.Fatalf("token 轮转不符: %v", auths)
	}
}

func TestGitHubRateLimitCooldown(t *testing.T) {
	stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	eng := NewGitHub(GitHubOpts{Enabled: true})
	if _, err := eng.Search("q", 1, antirobot.TimeRangeNone); err == nil {
		t.Fatal("429 应返回错误")
	}
	// 冷却期内直接拒绝，不打上游
	if _, err := eng.Search("q", 1, antirobot.TimeRangeNone); err == nil || !contains(err.Error(), "cooling down") {
		t.Fatalf("冷却期内应报 cooling down: %v", err)
	}
}

func TestGitHubForbiddenEntersCooldown(t *testing.T) {
	srv := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-reset", "9999999999")
		w.WriteHeader(http.StatusForbidden)
	})
	_ = srv
	eng := NewGitHub(GitHubOpts{Enabled: true})
	if _, err := eng.Search("q", 1, antirobot.TimeRangeNone); err == nil {
		t.Fatal("403 应返回错误")
	}
	if rest := eng.(*githubEngine).cooldownRemaining(); rest <= 0 {
		t.Fatal("403 应进入冷却")
	}
}

func TestGitHubBuildURLTimeRangeAndPaging(t *testing.T) {
	eng := NewGitHub(GitHubOpts{Enabled: true, NumResults: 5}).(*githubEngine)
	u := eng.buildURL("query", 2, antirobot.TimeRangeMonth)
	if !contains(u, "page=2") || !contains(u, "per_page=5") || !contains(u, "created%3A") {
		t.Fatalf("翻页/时间限定符缺失: %s", u)
	}
	// 超长查询截断到前 5 个子句
	long := strings.Repeat("word ", 60) // 60 子句，总长 > 256
	longURL := eng.buildURL(long, 1, antirobot.TimeRangeNone)
	if strings.Count(longURL, "word") != 5 {
		t.Fatalf("超长查询应截断到 5 个子句: %s", longURL)
	}
}

func TestGitHubTruncateClausesQuoteAware(t *testing.T) {
	// 引号短语计一个子句：截断保留配对引号，不会切出不配对引号（GitHub 422）
	q := `"hello world" a b c d e f g`
	got := truncateClauses(q)
	if got != `"hello world" a b c d` {
		t.Fatalf("引号短语应计一个子句: %q", got)
	}
	if strings.Count(got, `"`)%2 != 0 {
		t.Fatalf("截断结果引号不配对: %q", got)
	}
	// 未超限原样返回
	same := `foo "bar baz" qux`
	if got := truncateClauses(same); got != same {
		t.Fatalf("未超限不应改动: %q", got)
	}
}
