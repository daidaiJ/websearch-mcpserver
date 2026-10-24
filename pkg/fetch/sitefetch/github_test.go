package sitefetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
