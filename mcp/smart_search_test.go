package mcpserver

import (
	"context"
	"strings"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search"
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
		wantErr  bool
	}{
		{"academic only with depth", false, true, nil, "deep", nil, true},
		{"academic only balanced", false, true, nil, "balanced", nil, false},
		{"academic only fetch", false, true, &n, "", nil, true},
		{"academic only max chars", false, true, nil, "", &chars, true},
		{"mixed sources allow presets", true, true, &n, "deep", &chars, false},
		{"web only allow presets", true, false, &n, "deep", &chars, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSourcePresets(tc.web, tc.academic, tc.fetch, tc.depth, tc.maxChars)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDoSmartSearchAcademicSource(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	searchapi = &mockSearch{name: "mock", results: []search.SearchResult{{Title: "web hit", Url: "https://example.com"}}}
	academicSearcher = &mockAcademic{engines: []string{"arxiv"}, results: []search.SearchResult{{Title: "paper hit", Url: "https://arxiv.org/abs/1"}}}
	cacheInst = nil

	res, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{Query: "q", Sources: []string{"web", "academic"}})
	if err != nil {
		t.Fatal(err)
	}
	text := callToolText(res)
	if !strings.Contains(text, "## 学术来源") || !strings.Contains(text, "paper hit") {
		t.Fatalf("academic section missing: %q", text)
	}
}
