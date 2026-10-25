package sitefetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMatchGitHub(t *testing.T) {
	yes := []string{
		"https://github.com/golang/go",
		"https://github.com/golang/go/blob/master/README.md",
		"https://GITHUB.com/golang/go",
	}
	no := []string{
		"https://api.github.com/repos/golang/go",
		"https://github.dev/golang/go",
		"https://raw.githubusercontent.com/golang/go/master/README.md",
		"https://example.com/github.com/golang/go",
	}
	for _, u := range yes {
		if !MatchGitHub(u) {
			t.Errorf("应匹配: %s", u)
		}
	}
	for _, u := range no {
		if MatchGitHub(u) {
			t.Errorf("不应匹配: %s", u)
		}
	}
}

func TestGitHubFetcherFetchRepo(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/golang/go"):
			gotAuth = r.Header.Get("Authorization")
			gotAccept = r.Header.Get("Accept")
			_, _ = w.Write([]byte(`{"full_name":"golang/go","description":"The Go programming language","stargazers_count":125000,"language":"Go","license":{"spdx_id":"BSD-3-Clause"},"pushed_at":"2026-10-01T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/repos/golang/go/readme"):
			_, _ = w.Write([]byte("# Go\n\nOfficial docs."))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI := apiEndpoint
	apiEndpoint = srv.URL
	t.Cleanup(func() { apiEndpoint = oldAPI })

	f := NewGitHubFetcher([]string{"pat-1"})
	text, err := f.Fetch(context.Background(), "https://github.com/golang/go")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotAuth != "Bearer pat-1" {
		t.Fatalf("PAT 应注入 Authorization: %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Fatalf("API Accept 头不符: %q", gotAccept)
	}
	for _, want := range []string{"# golang/go", "The Go programming language", "★ 125000", "BSD-3-Clause", "最近推送 2026-10-01", "# Go\n\nOfficial docs."} {
		if !strings.Contains(text, want) {
			t.Errorf("输出缺 %q:\n%s", want, text)
		}
	}
}

func TestGitHubFetcherBlobToRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/repos/golang/go/contents/README.md") && r.URL.Query().Get("ref") == "master" {
			_, _ = w.Write([]byte("# hello raw"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	oldAPI := apiEndpoint
	apiEndpoint = srv.URL
	t.Cleanup(func() { apiEndpoint = oldAPI })

	f := NewGitHubFetcher(nil)
	text, err := f.Fetch(context.Background(), "https://github.com/golang/go/blob/master/README.md")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if text != "# hello raw" {
		t.Fatalf("blob 应走 contents API 取源文件: %q", text)
	}
}

func TestGitHubFetcherUnsupportedPath(t *testing.T) {
	f := NewGitHubFetcher(nil)
	if _, err := f.Fetch(context.Background(), "https://github.com/golang/go/issues/123"); err == nil {
		t.Fatal("issues 页不应接管")
	}
	if _, err := f.Fetch(context.Background(), "https://github.com/golang"); err == nil {
		t.Fatal("缺 repo 路径不应接管")
	}
}

func TestGitHubFetcherCooldownOn403(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	oldAPI := apiEndpoint
	apiEndpoint = srv.URL
	t.Cleanup(func() { apiEndpoint = oldAPI })

	f := NewGitHubFetcher(nil)
	if _, err := f.Fetch(context.Background(), "https://github.com/golang/go"); err == nil {
		t.Fatal("403 应返回错误")
	}
	if f.cooldownRemaining() <= 0 {
		t.Fatal("403 应进入配额冷却")
	}
	_, err := f.Fetch(context.Background(), "https://github.com/golang/go")
	if err == nil || !strings.Contains(err.Error(), "冷却") {
		t.Fatalf("冷却中应快速失败并说明原因: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("冷却中不应再请求 API（hits=%d）", hits.Load())
	}
}

func TestGitHubFetcherShortPushedAtNoPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/x/y"):
			// pushed_at 畸形短串：裸切片 [:10] 会越界 panic
			_, _ = w.Write([]byte(`{"full_name":"x/y","pushed_at":"2026"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI := apiEndpoint
	apiEndpoint = srv.URL
	t.Cleanup(func() { apiEndpoint = oldAPI })

	f := NewGitHubFetcher(nil)
	text, err := f.Fetch(context.Background(), "https://github.com/x/y")
	if err != nil {
		t.Fatalf("畸形 pushed_at 不应致命: %v", err)
	}
	if !strings.Contains(text, "最近推送 2026") {
		t.Errorf("短 pushed_at 应原样展示: %s", text)
	}
}

func TestGitHubFetcherBlobPathEscaping(t *testing.T) {
	var escapedPath, ref string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		ref = r.URL.Query().Get("ref")
		if strings.Contains(r.URL.Path, "/repos/golang/go/contents/docs/a b.md") {
			_, _ = w.Write([]byte("# raw with space"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	oldAPI := apiEndpoint
	apiEndpoint = srv.URL
	t.Cleanup(func() { apiEndpoint = oldAPI })

	f := NewGitHubFetcher(nil)
	text, err := f.Fetch(context.Background(), "https://github.com/golang/go/blob/master/docs/a%20b.md")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if text != "# raw with space" {
		t.Fatalf("含空格路径应正确编码并取回原文: %q", text)
	}
	if !strings.Contains(escapedPath, "a%20b.md") {
		t.Errorf("路径段应以 %%20 编码: %s", escapedPath)
	}
	if ref != "master" {
		t.Errorf("ref 应保留在 query: %q", ref)
	}
}

func TestMatchGitHubWWW(t *testing.T) {
	yes := []string{
		"https://www.github.com/golang/go",
		"https://WWW.GITHUB.COM/golang/go",
	}
	no := []string{
		"https://github.com.evil.example/golang/go",
		"https://mygithub.com/golang/go",
	}
	for _, u := range yes {
		if !MatchGitHub(u) {
			t.Errorf("应匹配（www 前缀归一）: %s", u)
		}
	}
	for _, u := range no {
		if MatchGitHub(u) {
			t.Errorf("不应匹配: %s", u)
		}
	}
}
