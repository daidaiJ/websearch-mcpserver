package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/search"
	searchcore "websearch/pkg/search/core"
	"websearch/pkg/telemetry"
)

func TestApplyContentBudget(t *testing.T) {
	original := []search.SearchResult{
		{Title: "a", Content: strings.Repeat("a", 1000)},
		{Title: "b", Content: strings.Repeat("b", 100)},
		{Title: "saved", Content: "notice", ContentSaved: true},
	}
	got := applyContentBudget(original, 900)
	if got[0].Content == original[0].Content {
		t.Fatal("long result was not trimmed")
	}
	if got[1].Content != original[1].Content {
		t.Fatal("short result should keep its full text")
	}
	if got[2].Content != original[2].Content {
		t.Fatal("saved-body notice must not be trimmed")
	}
	if !strings.Contains(got[0].Content, "max_chars") {
		t.Fatalf("trim marker missing: %q", got[0].Content)
	}
}

func TestNormalizeQueries(t *testing.T) {
	if _, err := normalizeQueries("", nil); err == nil {
		t.Fatal("empty query should fail")
	}
	if _, err := normalizeQueries("q", []string{"q2"}); err == nil {
		t.Fatal("query and queries together should fail")
	}
	if got, err := normalizeQueries("q", []string{""}); err != nil || len(got) != 1 || got[0] != "q" {
		t.Fatalf("blank queries should be ignored: %v, %v", got, err)
	}
	queries, err := normalizeQueries("", []string{" q1 ", "q1", "", "q2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != "q1" || queries[1] != "q2" {
		t.Fatalf("queries = %v", queries)
	}
	if _, err := normalizeQueries("", []string{"1", "2", "3", "4", "5", "6"}); err == nil {
		t.Fatal("six queries should exceed the batch cap")
	}
}

func TestResolveSearchDepth(t *testing.T) {
	n, budget, err := resolveSearchDepth("fast", nil, nil)
	if err != nil || n != 0 || budget != 0 {
		t.Fatalf("fast = %d, %d, %v", n, budget, err)
	}
	explicit := 4
	n, _, err = resolveSearchDepth("fast", &explicit, nil)
	if err != nil || n != 4 {
		t.Fatalf("explicit fetch_top_n must win: %d, %v", n, err)
	}
	n, budget, err = resolveSearchDepth("deep", nil, nil)
	if err != nil || n != 3 || budget != deepSearchContentChars {
		t.Fatalf("deep = %d, %d, %v", n, budget, err)
	}
	chars := 1200
	n, budget, err = resolveSearchDepth("deep", nil, &chars)
	if err != nil || n != 3 || budget != 1200 {
		t.Fatalf("explicit max_chars must win: %d, %d, %v", n, budget, err)
	}
	if _, _, err := resolveSearchDepth("turbo", nil, nil); err == nil {
		t.Fatal("unknown depth should fail")
	}
}

func TestNormalizeSources(t *testing.T) {
	web, academic, err := normalizeSources(nil)
	if err != nil || !web || academic {
		t.Fatalf("default = %v, %v, %v", web, academic, err)
	}
	web, academic, err = normalizeSources([]string{"web", "academic"})
	if err != nil || !web || !academic {
		t.Fatalf("both = %v, %v, %v", web, academic, err)
	}
	if _, _, err := normalizeSources([]string{"wiki"}); err == nil {
		t.Fatal("unsupported source should fail")
	}
}

func TestDoSmartSearchBatch(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	searchapi = &mockSearch{name: "mock", results: []search.SearchResult{{Title: "hit", Url: "https://example.com"}}}
	cacheInst = nil
	academicSearcher = nil

	res, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{Queries: []string{"q1", "q2"}})
	if err != nil {
		t.Fatal(err)
	}
	text := callToolText(res)
	if !strings.Contains(text, "query=q1") || !strings.Contains(text, "query=q2") {
		t.Fatalf("batch output missing sections: %q", text)
	}
	if !strings.Contains(text, "\n\n---\n\n") {
		t.Fatalf("batch output missing separator: %q", text)
	}
}

func TestSmartSearchBatchOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	searchapi = &mockSearch{name: "mock", results: []search.SearchResult{{Title: "hit", Url: "https://example.com"}}}

	code, _, msg := mcpHTTPPost(t, srv.Client(), srv.URL+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"queries":["q1","q2"],"depth":"fast"}}}`)
	if code != 200 || msg["error"] != nil {
		t.Fatalf("batch call: %d %v", code, msg)
	}
	text := jsonToolText(t, msg)
	if !strings.Contains(text, "query=q1") || !strings.Contains(text, "query=q2") {
		t.Fatalf("batch output missing sections: %q", text)
	}

	code, _, msg = mcpHTTPPost(t, srv.Client(), srv.URL+"/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if code != 200 || msg["error"] != nil {
		t.Fatalf("tools/list: %d %v", code, msg)
	}
	for _, tool := range msg["result"].(map[string]any)["tools"].([]any) {
		entry := tool.(map[string]any)
		if entry["name"] != "smartsearch" {
			continue
		}
		props := entry["inputSchema"].(map[string]any)["properties"].(map[string]any)
		for _, name := range []string{"queries", "depth", "sources", "max_chars"} {
			if _, ok := props[name]; !ok {
				t.Fatalf("smartsearch schema missing %q", name)
			}
		}
		return
	}
	t.Fatal("smartsearch tool not listed")
}

func TestSmartSearchMixedFailureOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	searchapi = &mockSearch{name: "mock", err: errors.New("web offline")}
	academicSearcher = &mockAcademic{err: errors.New("academic offline")}

	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"q","sources":["web","academic"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"smartsearch","arguments":{"queries":["q1","q2"],"sources":["web","academic"]}}}`,
	} {
		code, _, msg := mcpHTTPPost(t, srv.Client(), srv.URL+"/mcp", payload)
		result, _ := msg["result"].(map[string]any)
		if code != 200 || result["isError"] != true {
			t.Fatalf("want MCP tool error, got status %d and response %v", code, msg)
		}
		if got := jsonToolText(t, msg); !strings.Contains(got, "web offline") || !strings.Contains(got, "academic offline") {
			t.Fatalf("both source errors should be visible: %q", got)
		}
	}
}

func TestValidateSourcePresets(t *testing.T) {
	n := 3
	chars := 1000
	cases := []struct {
		name     string
		web      bool
		academic bool
		fetch    *int
		depth    string
		maxChars *int
		filter   searchcore.DomainFilter
		wantErr  bool
	}{
		{"academic only with depth", false, true, nil, "deep", nil, searchcore.DomainFilter{}, true},
		{"academic only balanced", false, true, nil, "balanced", nil, searchcore.DomainFilter{}, false},
		{"academic only fetch", false, true, &n, "", nil, searchcore.DomainFilter{}, true},
		{"academic only max chars", false, true, nil, "", &chars, searchcore.DomainFilter{}, true},
		{"academic only domain filter", false, true, nil, "", nil, searchcore.DomainFilter{Include: []string{"example.com"}}, true},
		{"mixed sources allow presets", true, true, &n, "deep", &chars, searchcore.DomainFilter{Include: []string{"example.com"}}, false},
		{"web only allow presets", true, false, &n, "deep", &chars, searchcore.DomainFilter{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSourcePresets(tc.web, tc.academic, tc.fetch, tc.depth, tc.maxChars, tc.filter)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDoSmartSearchAcademicSource(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst, fallbackSearch, summarizerInst = nil, nil, nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = telemetry.WithRequestID(ctx, "mixed-request")
	requestDone := ctx.Done()
	webStarted, academicStarted := make(chan struct{}), make(chan struct{})
	checkContext := func(ctx context.Context) error {
		if telemetry.RequestID(ctx) != "mixed-request" {
			return errors.New("request ID was not forwarded")
		}
		if ctx.Done() != requestDone {
			return errors.New("request cancellation was not shared")
		}
		return nil
	}
	searchapi = &smartSearchTestWeb{run: func(ctx context.Context, _ string) ([]search.SearchResult, error) {
		close(webStarted)
		select {
		case <-academicStarted:
			return []search.SearchResult{{Title: "web hit", Url: "https://example.com"}}, checkContext(ctx)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	academicSearcher = &smartSearchTestAcademic{run: func(ctx context.Context, _ string) (search.AcademicSearchResult, error) {
		select {
		case <-webStarted:
		case <-ctx.Done():
			return search.AcademicSearchResult{}, ctx.Err()
		}
		close(academicStarted)
		return search.AcademicSearchResult{Results: []search.SearchResult{{Title: "paper hit", Url: "https://arxiv.org/abs/1"}}}, checkContext(ctx)
	}}

	res, _, err := doSmartSearch(ctx, nil, smartSearchRequest{Query: "q", Depth: "fast", Sources: []string{"academic", "web"}})
	if err != nil {
		t.Fatal(err)
	}
	text := callToolText(res)
	webIndex, academicIndex := strings.Index(text, "web hit"), strings.Index(text, "## 学术来源")
	if webIndex < 0 || academicIndex < webIndex || !strings.Contains(text, "paper hit") {
		t.Fatalf("response should contain web then academic results: %q", text)
	}
}

func TestDoSmartSearchAcademicEmptyIsSuccess(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst, fallbackSearch, summarizerInst = nil, nil, nil
	smartSearchConf = config.SmartSearchConfig{}
	searchapi = &smartSearchTestWeb{run: func(context.Context, string) ([]search.SearchResult, error) {
		return nil, errors.New("web must not run")
	}}
	academicSearcher = &smartSearchTestAcademic{run: func(context.Context, string) (search.AcademicSearchResult, error) {
		return search.AcademicSearchResult{}, nil
	}}

	res, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{Query: "no hits", Sources: []string{"academic"}})
	if err != nil {
		t.Fatalf("academic empty result must not fail: %v", err)
	}
	if text := callToolText(res); !strings.Contains(text, i18n.T("search.academic.no_results")) {
		t.Fatalf("empty result text = %q", text)
	}
}

type smartSearchTestWeb struct {
	mockSearch
	run func(context.Context, string) ([]search.SearchResult, error)
}

func (s *smartSearchTestWeb) SearchRawContext(ctx context.Context, query string) ([]search.SearchResult, error) {
	return s.run(ctx, query)
}

type smartSearchTestAcademic struct {
	mockAcademic
	run func(context.Context, string) (search.AcademicSearchResult, error)
}

func (s *smartSearchTestAcademic) SearchAcademicRawContext(ctx context.Context, query string, _ ...search.AcademicSearchOptions) (search.AcademicSearchResult, error) {
	return s.run(ctx, query)
}

func TestDoSmartSearchMixedSourceFailures(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst, fallbackSearch, summarizerInst = nil, nil, nil
	smartSearchConf = config.SmartSearchConfig{}
	webErr := errors.New("web offline")
	academicErr := errors.New("academic offline")
	searchapi = &smartSearchTestWeb{run: func(_ context.Context, query string) ([]search.SearchResult, error) {
		if query == "web hit" {
			return []search.SearchResult{{Title: "web result", Url: "https://example.com"}}, nil
		}
		return nil, webErr
	}}
	academicSearcher = &smartSearchTestAcademic{run: func(_ context.Context, query string) (search.AcademicSearchResult, error) {
		switch query {
		case "academic hit":
			return search.AcademicSearchResult{Results: []search.SearchResult{{Title: "paper result", Url: "https://arxiv.org/abs/1"}}}, nil
		case "academic empty":
			return search.AcademicSearchResult{}, nil
		default:
			return search.AcademicSearchResult{}, academicErr
		}
	}}

	for _, tc := range []struct {
		name, query string
		queries     []string
		want        []string
		wantErr     []error
	}{
		{name: "both failed", query: "failure", want: []string{"网页检索失败", "学术检索失败"}, wantErr: []error{webErr, academicErr}},
		{name: "academic empty and web failed", query: "academic empty", want: []string{"网页检索失败"}, wantErr: []error{webErr}},
		{name: "web partial success", query: "web hit", want: []string{"web result", "academic offline"}},
		{name: "academic partial success", query: "academic hit", want: []string{"paper result", "web offline"}},
		{name: "batch all failed", queries: []string{"first failure", "second failure"}, want: []string{"first failure", "second failure"}, wantErr: []error{webErr, academicErr}},
		{name: "batch partial success", queries: []string{"failure", "web hit"}, want: []string{"web result", "academic offline", "failure:", "web offline"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{
				Query: tc.query, Queries: tc.queries, Depth: "fast", Sources: []string{"web", "academic"},
			})
			got := callToolText(res)
			if len(tc.wantErr) > 0 {
				if res != nil {
					t.Fatalf("got result for failed search: %q", got)
				}
				for _, cause := range tc.wantErr {
					if !errors.Is(err, cause) {
						t.Fatalf("error = %v, want cause %v", err, cause)
					}
				}
				got = err.Error()
			} else if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%q missing %q", got, want)
				}
			}
		})
	}
}
