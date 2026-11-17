package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/search"
	searchcore "websearch/pkg/search/core"
)

func TestDomainFilterToolSchemaExposed(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	academicSearcher = nil
	searchapi = &mockSearch{name: "mock"}
	webfetchInst = nil

	conf := config.Config{Bing: config.BingConfig{Enabled: true}}
	tool := toolByName(t, conf, "smartsearch")
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"include_domains", "exclude_domains"} {
		prop, ok := schema.Properties[name]
		if !ok {
			t.Fatalf("%s missing from smartsearch schema", name)
		}
		if prop.Description == "" || strings.HasPrefix(prop.Description, "mcp.param.") {
			t.Fatalf("%s description was not resolved: %q", name, prop.Description)
		}
	}
}

func TestNormalizeDomainFilters(t *testing.T) {
	filter, err := normalizeDomainFilters(
		[]string{"www.Example.com", "example.com", "*.docs.test", " "},
		[]string{"ads.example.com", "ADS.example.com"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(filter.Include, ","), "docs.test,example.com"; got != want {
		t.Fatalf("include = %q, want %q", got, want)
	}
	if got, want := strings.Join(filter.Exclude, ","), "ads.example.com"; got != want {
		t.Fatalf("exclude = %q, want %q", got, want)
	}

	if _, err := normalizeDomainFilters([]string{"not a host"}, nil); err == nil ||
		!strings.Contains(err.Error(), "include_domains") || !strings.Contains(err.Error(), "not a host") {
		t.Fatalf("invalid domain error should name the parameter and input: %v", err)
	}

	tooMany := make([]string, maxDomainPatterns+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("d%d.test", i)
	}
	if _, err := normalizeDomainFilters(tooMany, nil); err == nil || !strings.Contains(err.Error(), "include_domains") {
		t.Fatalf("oversized list should be rejected: %v", err)
	}
}

func TestDoSmartSearchAppliesDomainFilter(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst = nil
	academicSearcher = nil
	searchapi = &mockSearch{name: "mock", results: []search.SearchResult{
		{Title: "excluded", Url: "https://ads.example.com/excluded", Content: "excluded body"},
		{Title: "kept", Url: "https://example.com/kept", Content: "kept body"},
		{Title: "other", Url: "https://other.test/other", Content: "other body"},
	}}

	res, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{
		Query:          "q",
		FetchTopN:      intPtr(0),
		ExcludeDomains: []string{"example.com", "ads.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := callToolText(res)
	if strings.Contains(text, "ads.example.com") || strings.Contains(text, "https://example.com/kept") {
		t.Fatalf("excluded domains leaked into response: %s", text)
	}
	if !strings.Contains(text, "other.test") {
		t.Fatalf("unfiltered result missing: %s", text)
	}
}

type ctxCapturingSearch struct {
	mockSearch
	gotFilter searchcore.DomainFilter
	searched  bool
}

func (s *ctxCapturingSearch) SearchRawContextDiagnostics(ctx context.Context, query string) ([]search.SearchResult, searchcore.SearchDiagnostics, error) {
	s.gotFilter = searchcore.DomainFilterFromContext(ctx)
	s.searched = true
	return s.results, searchcore.SearchDiagnostics{}, nil
}

func (s *ctxCapturingSearch) SearchRawWithTimeRangeContextDiagnostics(ctx context.Context, query string, lookbackDays int) ([]search.SearchResult, searchcore.SearchDiagnostics, error) {
	return s.SearchRawContextDiagnostics(ctx, query)
}

func TestDoSmartSearchForwardsDomainFilterToOrchestrator(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst = nil
	academicSearcher = nil
	s := &ctxCapturingSearch{mockSearch: mockSearch{name: "mock", results: []search.SearchResult{
		{Title: "kept", Url: "https://example.com/kept"},
	}}}
	searchapi = s

	if _, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{
		Query:          "q",
		FetchTopN:      intPtr(0),
		IncludeDomains: []string{"Example.com", "www.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if !s.searched {
		t.Fatal("orchestrator was not called")
	}
	if got := strings.Join(s.gotFilter.Include, ","); got != "example.com" {
		t.Fatalf("orchestrator filter include = %q, want example.com", got)
	}
	if len(s.gotFilter.Exclude) != 0 {
		t.Fatalf("orchestrator filter exclude = %v, want empty", s.gotFilter.Exclude)
	}
}

func TestDoSmartSearchRejectsDomainFilterForAcademicOnly(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst = nil
	searchapi = &mockSearch{name: "mock"}
	academicSearcher = nil

	_, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{
		Query:          "q",
		Sources:        []string{"academic"},
		IncludeDomains: []string{"example.com"},
	})
	if err == nil || !strings.Contains(err.Error(), "include_domains") {
		t.Fatalf("academic-only request should reject domain filters: %v", err)
	}
}

type countingSearch struct {
	mockSearch
	calls int
}

func (s *countingSearch) SearchRaw(query string) ([]search.SearchResult, error) {
	s.calls++
	return s.mockSearch.SearchRaw(query)
}

func TestDomainFilterCacheIsolation(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "domain-filter-test"
	summarizerInst, fallbackSearch = nil, nil
	smartSearchConf = config.SmartSearchConfig{}
	s := &countingSearch{mockSearch: mockSearch{name: "mock", results: []search.SearchResult{
		{Title: "hit", Url: "https://example.com/hit"},
		{Title: "other", Url: "https://other.test/hit"},
	}}}
	searchapi = s

	call := func(include, exclude []string) {
		t.Helper()
		if _, _, err := doSmartSearch(context.Background(), nil, smartSearchRequest{
			Query: "q", FetchTopN: intPtr(0), IncludeDomains: include, ExcludeDomains: exclude,
		}); err != nil {
			t.Fatal(err)
		}
	}
	call([]string{"example.com"}, nil)
	call([]string{"other.test"}, nil)
	call([]string{"example.com"}, nil) // cache hit
	if s.calls != 2 {
		t.Fatalf("filtered cache did not isolate results: calls = %d, want 2", s.calls)
	}
	call(nil, nil) // unfiltered key must not reuse filtered entries
	if s.calls != 3 {
		t.Fatalf("unfiltered request reused a filtered cache entry: calls = %d, want 3", s.calls)
	}
	call([]string{"example.com"}, []string{"ads.example.com"}) // exclude changes the key
	if s.calls != 4 {
		t.Fatalf("exclude list did not partition cache: calls = %d, want 4", s.calls)
	}
}
