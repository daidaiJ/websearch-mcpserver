package sitefetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMatchStackExchange(t *testing.T) {
	yes := []string{
		"https://stackoverflow.com/questions/12345/how-to-test",
		"https://stackoverflow.com/q/12345",
		"https://serverfault.com/questions/1",
		"https://meta.stackoverflow.com/questions/2",
		"https://math.stackexchange.com/questions/3/title",
		"https://mathoverflow.net/questions/4",
	}
	no := []string{
		"https://stackoverflow.com/users/12345",
		"https://stackoverflow.com/questions",
		"https://example.com/questions/12345",
		"https://stackoverflow.com.evil.example/questions/1",
	}
	for _, u := range yes {
		if !NewStackExchangeFetcher().Match(u) {
			t.Errorf("should match: %s", u)
		}
	}
	for _, u := range no {
		if NewStackExchangeFetcher().Match(u) {
			t.Errorf("should not match: %s", u)
		}
	}
}

func TestStackExchangeFetcherAcceptedAnswerFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/questions/12345" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("site"); got != "stackoverflow" {
			t.Errorf("site = %q", got)
		}
		if got := r.URL.Query().Get("filter"); got == "" {
			t.Error("missing filter")
		}
		_, _ = w.Write([]byte(`{"items":[{
			"question_id":12345,
			"title":"How to test?",
			"link":"https://stackoverflow.com/questions/12345",
			"body_markdown":"Question body",
			"score":10,
			"answer_count":2,
			"tags":["go","testing"],
			"answers":[
				{"score":1,"is_accepted":true,"body_markdown":"Accepted answer","owner":{"display_name":"alice"},"comments":[{"score":2,"body":"thanks","owner":{"display_name":"bob"}}]},
				{"score":50,"is_accepted":false,"body_markdown":"Popular answer","owner":{"display_name":"carol"}}
			]
		}]}`))
	}))
	t.Cleanup(srv.Close)
	old := seAPIEndpoint
	seAPIEndpoint = srv.URL
	t.Cleanup(func() { seAPIEndpoint = old })

	text, err := NewStackExchangeFetcher().Fetch(context.Background(), "https://stackoverflow.com/questions/12345/how-to-test")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, want := range []string{"# How to test?", "Question body", "标签: go, testing", "已采纳", "alice", "thanks", "Popular answer"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "Accepted answer") > strings.Index(text, "Popular answer") {
		t.Errorf("accepted answer should render before the higher-score answer:\n%s", text)
	}
}

func TestStackExchangeFetcherEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	old := seAPIEndpoint
	seAPIEndpoint = srv.URL
	t.Cleanup(func() { seAPIEndpoint = old })

	if _, err := NewStackExchangeFetcher().Fetch(context.Background(), "https://stackoverflow.com/questions/1"); err == nil {
		t.Fatal("empty items should fail so the caller can fall back")
	}
}

func TestMatchWikipedia(t *testing.T) {
	yes := []string{
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
		"https://zh.wikipedia.org/wiki/%E4%B8%AD%E6%96%87",
		"https://zh-min-nan.wikipedia.org/wiki/T%C3%A2i-g%C3%AD",
		"https://en.m.wikipedia.org/wiki/Go",
	}
	no := []string{
		"https://en.wikipedia.org/w/index.php?title=Go",
		"https://en.wikipedia.org/",
		"https://example.com/wiki/Go",
		"https://wikipedia.org.evil.example/wiki/Go",
	}
	for _, u := range yes {
		if !NewWikipediaFetcher().Match(u) {
			t.Errorf("should match: %s", u)
		}
	}
	for _, u := range no {
		if NewWikipediaFetcher().Match(u) {
			t.Errorf("should not match: %s", u)
		}
	}
}

func TestWikipediaFetcherExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("titles"); got != "Go (programming language)" {
			t.Errorf("titles = %q", got)
		}
		if got := r.URL.Query().Get("explaintext"); got != "1" {
			t.Errorf("explaintext = %q", got)
		}
		_, _ = w.Write([]byte(`{"query":{"pages":[{"title":"Go (programming language)","extract":"Go is a statically typed language."}]}}`))
	}))
	t.Cleanup(srv.Close)
	old := wikiAPIURL
	wikiAPIURL = func(string) string { return srv.URL }
	t.Cleanup(func() { wikiAPIURL = old })

	text, err := NewWikipediaFetcher().Fetch(context.Background(), "https://en.wikipedia.org/wiki/Go_(programming_language)")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, want := range []string{"# Go (programming language)", "statically typed"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestWikipediaFetcherMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query":{"pages":[{"title":"Nope","missing":true}]}}`))
	}))
	t.Cleanup(srv.Close)
	old := wikiAPIURL
	wikiAPIURL = func(string) string { return srv.URL }
	t.Cleanup(func() { wikiAPIURL = old })

	if _, err := NewWikipediaFetcher().Fetch(context.Background(), "https://en.wikipedia.org/wiki/Nope"); err == nil {
		t.Fatal("missing article should fail so the caller can fall back")
	}
}

func TestMatchArxiv(t *testing.T) {
	yes := []string{
		"https://arxiv.org/abs/2401.12345",
		"https://arxiv.org/abs/2401.12345v2",
		"https://arxiv.org/html/2401.12345",
		"https://arxiv.org/abs/hep-th/9901001",
	}
	no := []string{
		"https://arxiv.org/pdf/2401.12345",
		"https://arxiv.org/list/cs.AI/recent",
		"https://export.arxiv.org/abs/2401.12345",
		"https://example.com/abs/2401.12345",
	}
	for _, u := range yes {
		if !NewArxivFetcher().Match(u) {
			t.Errorf("should match: %s", u)
		}
	}
	for _, u := range no {
		if NewArxivFetcher().Match(u) {
			t.Errorf("should not match: %s", u)
		}
	}
}

func TestArxivFetcherRendersEntry(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2401.12345v1</id>
    <published>2024-01-22T00:00:00Z</published>
    <title>A Useful Paper</title>
    <summary>We present a useful result.</summary>
    <author><name>Alice</name></author>
    <author><name>Bob</name></author>
    <arxiv:doi>10.1000/xyz</arxiv:doi>
    <arxiv:journal_ref>Journal of Tests</arxiv:journal_ref>
    <category term="cs.AI"/>
    <category term="cs.AI"/>
    <category term="cs.LG"/>
  </entry>
</feed>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id_list"); got != "2401.12345v1" {
			t.Errorf("id_list = %q", got)
		}
		_, _ = w.Write([]byte(feed))
	}))
	t.Cleanup(srv.Close)
	old := arxivAPIEndpoint
	arxivAPIEndpoint = srv.URL
	t.Cleanup(func() { arxivAPIEndpoint = old })

	text, err := NewArxivFetcher().Fetch(context.Background(), "https://arxiv.org/abs/2401.12345v1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, want := range []string{"# A Useful Paper", "Alice, Bob", "2024-01-22", "cs.AI, cs.LG", "10.1000/xyz", "Journal of Tests", "useful result"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "cs.AI") != 1 {
		t.Errorf("categories should be deduplicated:\n%s", text)
	}
}

func TestArxivFetcherEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	t.Cleanup(srv.Close)
	old := arxivAPIEndpoint
	arxivAPIEndpoint = srv.URL
	t.Cleanup(func() { arxivAPIEndpoint = old })

	if _, err := NewArxivFetcher().Fetch(context.Background(), "https://arxiv.org/abs/2401.12345"); err == nil {
		t.Fatal("empty feed should fail so the caller can fall back")
	}
}

func TestMatchSpecialistRoutesByHost(t *testing.T) {
	if provider, matched := MatchSpecialist("https://stackoverflow.com/questions/1"); !matched || provider != "stackexchange_api" {
		t.Fatalf("stackexchange route: matched=%v provider=%q", matched, provider)
	}
	if provider, matched := MatchSpecialist("https://en.wikipedia.org/wiki/Go"); !matched || provider != "wikipedia_api" {
		t.Fatalf("wikipedia route: matched=%v provider=%q", matched, provider)
	}
	if provider, matched := MatchSpecialist("https://arxiv.org/abs/2401.12345"); !matched || provider != "arxiv_api" {
		t.Fatalf("arxiv route: matched=%v provider=%q", matched, provider)
	}
	if _, matched := MatchSpecialist("https://example.com/page"); matched {
		t.Fatal("generic URL should not match a specialist")
	}
}
