package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type keenableCapture struct {
	path    string
	apiKey  string
	title   string
	body    keenableSearchReq
	query   string
	liveQ   string
	maxChar string
}

func newKeenableTestServer(t *testing.T, resp string, cap *keenableCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.apiKey = r.Header.Get("X-API-Key")
		cap.title = r.Header.Get("X-Keenable-Title")
		switch r.Method {
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.body)
		case http.MethodGet:
			cap.query = r.URL.Query().Get("url")
			cap.liveQ = r.URL.Query().Get("live")
			cap.maxChar = r.URL.Query().Get("max_chars")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
}

func TestKeenableSearchPublicTier(t *testing.T) {
	// 未配置 Key → public 端点 + X-Keenable-Title，不带 X-API-Key
	cap := &keenableCapture{}
	srv := newKeenableTestServer(t, `{"query":"q","mode":"pro","results":[
		{"title":"Go","url":"https://go.dev/","description":"desc","snippet":"片段","published_at":"2026-10-01T00:00:00Z"}
	]}`, cap)
	defer srv.Close()

	k := NewKeenableSearch(nil, false, 0)
	k.baseURL = srv.URL
	results, err := k.SearchRawContext(context.Background(), "go generics")
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/search/public" || cap.title != keenableAppTitle || cap.apiKey != "" {
		t.Fatalf("path=%q title=%q key=%q", cap.path, cap.title, cap.apiKey)
	}
	if cap.body.Query != "go generics" || cap.body.Mode != "pro" || cap.body.MaxResults != 10 {
		t.Fatalf("body = %+v", cap.body)
	}
	if len(results) != 1 || results[0].Content != "片段" || results[0].PublishDate != "2026-10-01T00:00:00Z" {
		t.Fatalf("results = %+v", results)
	}
}

func TestKeenableSearchKeyedTier(t *testing.T) {
	// 配置 Key → keyed 端点 + X-API-Key；401 标记 Key 无效
	cap := &keenableCapture{}
	status := http.StatusOK
	srv := newKeenableTestServer(t, `{"query":"q","mode":"pro","results":[{"title":"Go","url":"https://go.dev/","snippet":"s"}]}`, cap)
	defer srv.Close()
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.apiKey = r.Header.Get("X-API-Key")
		cap.title = r.Header.Get("X-Keenable-Title")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev/","snippet":"s"}]}`))
		}
	})

	pool, _ := NewKeyPool([]string{"keen-1"})
	k := NewKeenableSearch(pool, false, 0)
	k.baseURL = srv.URL
	if _, err := k.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	if cap.path != "/search" || cap.apiKey != "keen-1" || cap.title != "" {
		t.Fatalf("path=%q key=%q title=%q", cap.path, cap.apiKey, cap.title)
	}

	status = http.StatusUnauthorized
	_, err := k.SearchRaw("q")
	if err == nil {
		t.Fatal("401 must error")
	}
	var ke *KeyError
	if !asKeyError(err, &ke) || ke.Key != "keen-1" {
		t.Fatalf("err = %v, want KeyError(keen-1)", err)
	}
}

func TestKeenableFetch(t *testing.T) {
	cap := &keenableCapture{}
	srv := newKeenableTestServer(t, `{"url":"https://example.com","title":"Example","content":"正文内容","published_at":1768435200}`, cap)
	defer srv.Close()

	// public 档：/fetch/public + live 透传
	k := NewKeenableSearch(nil, true, 0)
	k.baseURL = srv.URL
	text, err := k.Fetch(context.Background(), "https://example.com", 100)
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/fetch/public" || cap.query != "https://example.com" || cap.liveQ != "true" || cap.maxChar != "100" {
		t.Fatalf("path=%q url=%q live=%q max=%q", cap.path, cap.query, cap.liveQ, cap.maxChar)
	}
	if !strings.Contains(text, "正文内容") {
		t.Fatalf("text = %q", text)
	}

	// keyed 档：/fetch + X-API-Key
	cap2 := &keenableCapture{}
	srv2 := newKeenableTestServer(t, `{"url":"https://example.com","content":"c"}`, cap2)
	defer srv2.Close()
	pool, _ := NewKeyPool([]string{"keen-1"})
	k2 := NewKeenableSearch(pool, false, 0)
	k2.baseURL = srv2.URL
	if _, err = k2.Fetch(context.Background(), "https://example.com", 0); err != nil {
		t.Fatal(err)
	}
	if cap2.path != "/fetch" || cap2.apiKey != "keen-1" || cap2.maxChar != "" {
		t.Fatalf("path=%q key=%q max=%q", cap2.path, cap2.apiKey, cap2.maxChar)
	}
}

func TestKeenableLimitClamp(t *testing.T) {
	if k := NewKeenableSearch(nil, false, 99); k.limit != 10 && (k.limit < 1 || k.limit > 50) {
		t.Fatalf("limit = %d", k.limit)
	}
	if k := NewKeenableSearch(nil, false, 25); k.limit != 25 {
		t.Fatalf("limit = %d, want 25", k.limit)
	}
	if k := NewKeenableSearch(nil, false, 0); k.limit != 10 {
		t.Fatalf("default limit = %d, want 10", k.limit)
	}
}
