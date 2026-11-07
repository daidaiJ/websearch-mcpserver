package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/search"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type timeRangeWebSearch struct {
	mockSearch
	days []int
}

func (s *timeRangeWebSearch) SearchRawWithTimeRange(query string, days int) ([]search.SearchResult, error) {
	s.days = append(s.days, days)
	return []search.SearchResult{{Title: fmt.Sprintf("window-%d-days", days), Url: "https://example.com", Content: strings.Repeat("synthetic content ", 100)}}, nil
}

func TestWebSearchCacheSeparatesTimeRanges(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "time-range-test"
	summarizerInst, fallbackSearch = nil, nil
	smartSearchConf = config.SmartSearchConfig{}
	s := &timeRangeWebSearch{mockSearch: mockSearch{name: "mock"}}
	searchapi = s
	for _, tc := range []struct {
		months, fetch, wantDays, wantCalls int
	}{
		{3, 0, 90, 1}, {6, 0, 180, 2}, {3, 0, 90, 2},
		{6, 1, 180, 2}, {3, 1, 90, 2}, {6, 1, 180, 2},
		{0, 0, 0, 3}, {3, 0, 90, 3}, {0, 1, 0, 3},
	} {
		var params SearchParamsNoIntent
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"query":"same query","time_range":%d,"fetch_top_n":%d}`, tc.months, tc.fetch)), &params); err != nil {
			t.Fatal(err)
		}
		result, _, err := WebSearchNoIntent(context.Background(), nil, &params)
		if err != nil {
			t.Fatal(err)
		}
		text := result.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(text, fmt.Sprintf("window-%d-days", tc.wantDays)) || len(s.days) != tc.wantCalls {
			t.Fatalf("months=%d fetch=%d: calls=%v, response=%s", tc.months, tc.fetch, s.days, text)
		}
	}
}

func TestWebSearchDistinguishesOmittedAndUnlimitedTimeRange(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	cacheInst, summarizerInst, fallbackSearch = nil, nil, nil
	smartSearchConf = config.SmartSearchConfig{}
	s := &timeRangeWebSearch{mockSearch: mockSearch{name: "mock"}}
	searchapi = s
	for _, tc := range []struct {
		args string
		want int
	}{
		{`{"query":"q","fetch_top_n":0}`, 90},
		{`{"query":"q","time_range":0,"fetch_top_n":0}`, 0},
		{`{"query":"q","time_range":null,"fetch_top_n":0}`, 90},
	} {
		var params SearchParamsNoIntent
		if err := json.Unmarshal([]byte(tc.args), &params); err != nil {
			t.Fatal(err)
		}
		if _, _, err := WebSearchNoIntent(context.Background(), nil, &params); err != nil {
			t.Fatal(err)
		}
		if got := s.days[len(s.days)-1]; got != tc.want {
			t.Fatalf("args=%s: lookback days=%d, want %d", tc.args, got, tc.want)
		}
	}
}

func TestWebSearchRejectsInvalidTimeRanges(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	s := &timeRangeWebSearch{mockSearch: mockSearch{name: "mock"}}
	searchapi = s
	for _, months := range []int{-1, int(^uint(0) >> 1)} {
		params := &SearchParamsNoIntent{searchBaseParams: searchBaseParams{Query: "q", TimeRange: &months}}
		if _, _, err := WebSearchNoIntent(context.Background(), nil, params); err == nil || len(s.days) != 0 {
			t.Fatalf("invalid months=%d reached search: calls=%v, err=%v", months, s.days, err)
		}
	}
}

func TestWebSearchDoesNotReuseLegacyCache(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "legacy-test"
	summarizerInst, fallbackSearch = nil, nil
	smartSearchConf = config.SmartSearchConfig{}
	view, err := c.ForRequest(cacheNamespace)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"same query", "same query|fetch_top_n=1"} {
		if err := view.Store(key, "", false, []search.SearchResult{{Title: "legacy wrong window", Url: "https://example.com"}}, "", time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	s := &timeRangeWebSearch{mockSearch: mockSearch{name: "mock"}}
	searchapi = s
	for _, fetch := range []int{1, 0} {
		if _, _, err := doWebSearch(context.Background(), nil, "same query", "", 6, &fetch); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.days) != 2 || s.days[0] != 180 || s.days[1] != 180 {
		t.Fatalf("legacy results reused: calls=%v", s.days)
	}
}

type blockedWebSearch struct {
	mockSearch
	started chan struct{}
	release chan struct{}
}

func (m *blockedWebSearch) SearchRaw(query string) ([]search.SearchResult, error) {
	close(m.started)
	<-m.release
	return m.mockSearch.SearchRaw(query)
}

type blockedAcademicSearch struct {
	mockAcademic
	started chan struct{}
	release chan struct{}
}

func (m *blockedAcademicSearch) SearchAcademicRaw(query string, opts ...search.AcademicSearchOptions) (search.AcademicSearchResult, error) {
	close(m.started)
	<-m.release
	return m.mockAcademic.SearchAcademicRaw(query, opts...)
}

func TestSearchCachesCannotRepopulateAfterClear(t *testing.T) {
	for _, academic := range []bool{false, true} {
		t.Run(map[bool]string{false: "web", true: "academic"}[academic], func(t *testing.T) {
			initTestLogger()
			restoreGlobals(t)
			c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			cacheInst, cacheNamespace = c, "source-a"
			summarizerInst, fallbackSearch = nil, nil
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			searchapi = &blockedWebSearch{mockSearch: mockSearch{name: "mock", results: []search.SearchResult{{Title: "web result", Url: "https://example.com"}}}, started: started, release: release}
			academicSearcher = &blockedAcademicSearch{mockAcademic: mockAcademic{engines: []string{"arxiv"}, results: []search.SearchResult{{Title: "paper", Url: "https://example.com/paper", Type: "paper"}}}, started: started, release: release}
			done := make(chan error, 1)
			go func() {
				if academic {
					_, _, err := doAcademicSearch(context.Background(), "q", nil, "", 1)
					done <- err
				} else {
					_, _, err := doWebSearch(context.Background(), nil, "q", "", 3, intPtr(0))
					done <- err
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("search never started")
			}
			if _, err := c.Clear(); err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			view, err := c.ForRequest("source-a")
			if err != nil {
				t.Fatal(err)
			}
			key := webSearchCacheQuery("q", 3, 0)
			if academic {
				// 与 tool_academic.go 的 academicCacheKey 保持一致（含 page）。
				key = academicCacheKey("q", "", nil, 1)
			}
			if rec, _, err := view.Lookup(key, "", academic); err != nil || rec != nil {
				t.Fatalf("old in-flight search populated current generation: %+v, %v", rec, err)
			}
			// A new request must still cache successfully after invalidation.
			searchapi = &mockSearch{name: "mock", results: []search.SearchResult{{Title: "current web", Url: "https://example.com"}}}
			academicSearcher = &mockAcademic{engines: []string{"arxiv"}, results: []search.SearchResult{{Title: "current paper", Url: "https://example.com/paper", Type: "paper"}}}
			if academic {
				_, _, err = doAcademicSearch(context.Background(), "q", nil, "", 1)
			} else {
				_, _, err = doWebSearch(context.Background(), nil, "q", "", 3, intPtr(0))
			}
			if err != nil {
				t.Fatal(err)
			}
			if rec, _, err := view.Lookup(key, "", academic); err != nil || rec == nil {
				t.Fatalf("new-generation request did not cache: %+v, %v", rec, err)
			}
		})
	}
}

func TestSearchCacheNamespaceTracksSourcesNotDashboard(t *testing.T) {
	conf := config.Config{Mode: config.ModeHybrid}
	conf.Tavily.APIKey = "synthetic-secret-never-persist"
	base, err := searchCacheNamespace(conf)
	if err != nil || len(base) != 64 || strings.Contains(base, conf.Tavily.APIKey) {
		t.Fatalf("namespace must be a hash, not secret material: %q, %v", base, err)
	}
	changed := conf
	changed.Dashboard.AdminPassword = "different-admin-password"
	if got, err := searchCacheNamespace(changed); err != nil || got != base {
		t.Fatalf("dashboard-only changes should not partition search results: %q, %v", got, err)
	}
	for name, change := range map[string]func(*config.Config){
		"api switch":      func(c *config.Config) { off := false; c.Anysearch.Enabled = &off },
		"web engine":      func(c *config.Config) { c.So360.Enabled = true },
		"academic engine": func(c *config.Config) { c.Academic.DisableArxiv = true },
		"mode":            func(c *config.Config) { c.Mode = config.ModeEngine },
		"doubao filter":   func(c *config.Config) { c.Doubao.ICPHostOnly = true },
		"doubao version":  func(c *config.Config) { c.Doubao.Version = "custom" },
	} {
		changed := conf
		change(&changed)
		if got, err := searchCacheNamespace(changed); err != nil || got == base {
			t.Fatalf("%s change must partition the cache: %q, %v", name, got, err)
		}
	}
}
