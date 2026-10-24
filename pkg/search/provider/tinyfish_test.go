package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewTinyFishSearchNilPool(t *testing.T) {
	// 无匿名档：nil KeyPool 必须返回 nil，由上层跳过该供应商
	if NewTinyFishSearch(nil) != nil {
		t.Fatal("nil pool must disable tinyfish (no keyless tier)")
	}
}

func TestTinyFishSearch(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"q","results":[
			{"position":1,"site_name":"go.dev","title":"Go","snippet":"正文","url":"https://go.dev/","date":"2026-10-01"}
		],"total_results":1,"page":0}`))
	}))
	defer srv.Close()

	pool, err := NewKeyPool([]string{"tfk-1"})
	if err != nil {
		t.Fatal(err)
	}
	tf := NewTinyFishSearch(pool)
	tf.searchURL = srv.URL
	results, err := tf.SearchRawContext(context.Background(), "go generics")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "tfk-1" {
		t.Fatalf("key=%q", gotKey)
	}
	if len(results) != 1 || results[0].Url != "https://go.dev/" || results[0].Title != "Go" {
		t.Fatalf("results = %+v", results)
	}
	if results[0].PublishDate != "2026-10-01" || results[0].DateSource != "structured" {
		t.Fatalf("date mapping: %+v", results[0])
	}
}

func TestTinyFishSearchAuthErrorMarksKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	pool, err := NewKeyPool([]string{"bad-key"})
	if err != nil {
		t.Fatal(err)
	}
	tf := NewTinyFishSearch(pool)
	tf.searchURL = srv.URL
	_, err = tf.SearchRaw("q")
	if err == nil {
		t.Fatal("401 must error")
	}
	var ke *KeyError
	if !asKeyError(err, &ke) || ke.Key != "bad-key" {
		t.Fatalf("err = %v, want KeyError(bad-key)", err)
	}
}

func TestTinyFishFetch(t *testing.T) {
	var gotBody tinyfishFetchReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com","title":"Example","text":"# Example\n\n正文内容"}],"errors":[]}`))
	}))
	defer srv.Close()

	pool, err := NewKeyPool([]string{"tfk-1"})
	if err != nil {
		t.Fatal(err)
	}
	tf := NewTinyFishSearch(pool)
	tf.fetchURL = srv.URL
	text, err := tf.Fetch(context.Background(), "https://example.com", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotBody.URLs) != 1 || gotBody.URLs[0] != "https://example.com" || gotBody.Format != "markdown" {
		t.Fatalf("req body = %+v", gotBody)
	}
	if !strings.Contains(text, "正文内容") {
		t.Fatalf("text = %q", text)
	}

	// maxChars 截断
	text, err = tf.Fetch(context.Background(), "https://example.com", 5)
	if err != nil || len([]rune(text)) != 5 {
		t.Fatalf("truncated=%q err=%v", text, err)
	}

	// errors[] 随 200 返回时须报错（级联下一个 fetcher）
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"errors":[{"url":"https://example.com","error":"bot_blocked","status":403}]}`))
	}))
	defer srv2.Close()
	tf.fetchURL = srv2.URL
	if _, err = tf.Fetch(context.Background(), "https://example.com", 0); err == nil || !strings.Contains(err.Error(), "bot_blocked") {
		t.Fatalf("err = %v, want bot_blocked", err)
	}
}

func asKeyError(err error, target **KeyError) bool {
	ke, ok := err.(*KeyError)
	if ok {
		*target = ke
		return true
	}
	return false
}
