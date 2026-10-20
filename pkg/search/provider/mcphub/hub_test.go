package mcphub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/search/core"
)

type fakeCall struct {
	server string
	tool   string
	key    string
}

type fakeCaller struct {
	mu      sync.Mutex
	calls   []fakeCall
	results map[string]toolResult
	errs    map[string]error
}

func (f *fakeCaller) Call(_ context.Context, s *server, tool string, _ map[string]any, _ time.Duration, key string) (toolResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{server: s.name, tool: tool, key: key})
	f.mu.Unlock()
	if err := f.errs[s.name+"/"+tool]; err != nil {
		return toolResult{}, err
	}
	if err := f.errs[s.name]; err != nil {
		return toolResult{}, err
	}
	if res, ok := f.results[s.name+"/"+tool]; ok {
		return res, nil
	}
	return f.results[s.name], nil
}

func (f *fakeCaller) snapshot() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall(nil), f.calls...)
}

func testProxy() string { return "http://127.0.0.1:7897" }

func testConfig(servers ...string) config.MCPHubConfig {
	cfg := config.MCPHubConfig{CooldownSec: 1}
	for _, name := range servers {
		cfg.Servers = append(cfg.Servers, config.MCPHubServerConfig{Name: name})
	}
	return cfg
}

func parallelToolResult(url, title string) toolResult {
	return toolResult{Structured: map[string]any{"results": []any{
		map[string]any{"url": url, "title": title, "excerpts": []any{"body"}},
	}}}
}

func exaResult(title, url string) toolResult {
	return toolResult{Text: fmt.Sprintf("Title: %s\nURL: %s\nPublished: N/A\nAuthor: N/A\nHighlights:\nbody", title, url)}
}

func TestHubFallsBackAndCoolsFailedServer(t *testing.T) {
	fake := &fakeCaller{
		errs: map[string]error{"parallel": errors.New("connection refused")},
		results: map[string]toolResult{
			"exa": exaResult("Exa", "https://example.com/exa"),
		},
	}
	h, err := newHub(testConfig("parallel", "exa"), testProxy, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.SearchRaw("q")
	if err != nil || len(got) != 1 || got[0].Engine != "exa_mcp" {
		t.Fatalf("results=%+v err=%v", got, err)
	}
	got, err = h.SearchRaw("q")
	if err != nil || len(got) != 1 {
		t.Fatalf("second results=%+v err=%v", got, err)
	}
	calls := fake.snapshot()
	if len(calls) != 3 || calls[0].server != "parallel" || calls[1].server != "exa" || calls[2].server != "exa" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestHubRoundRobin(t *testing.T) {
	fake := &fakeCaller{results: map[string]toolResult{
		"parallel": parallelToolResult("https://example.com/p", "P"),
		"exa":      exaResult("E", "https://example.com/e"),
	}}
	cfg := testConfig("parallel", "exa")
	cfg.Strategy = "round-robin"
	h, err := newHub(cfg, testProxy, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	calls := fake.snapshot()
	if len(calls) != 2 || calls[0].server != "parallel" || calls[1].server != "exa" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestHubAllCooledFailsFast(t *testing.T) {
	fake := &fakeCaller{errs: map[string]error{
		"parallel": errors.New("connection refused"),
		"exa":      errors.New("timeout"),
	}}
	h, err := newHub(testConfig("parallel", "exa"), testProxy, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err == nil {
		t.Fatal("expected first failure")
	}
	before := len(fake.snapshot())
	if _, err := h.SearchRaw("q"); err == nil {
		t.Fatal("expected cooled failure")
	}
	if after := len(fake.snapshot()); after != before {
		t.Fatalf("cooled hub should not call servers again: before=%d after=%d", before, after)
	}
}

func TestHubFetchFallback(t *testing.T) {
	fake := &fakeCaller{
		errs: map[string]error{"parallel/web_fetch": errors.New("connection reset")},
		results: map[string]toolResult{
			"exa/web_fetch_exa": {Text: "# Ex\nURL: https://example.com\n\nbody"},
		},
	}
	h, err := newHub(testConfig("parallel", "exa"), testProxy, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, err := h.Fetch(context.Background(), "https://example.com")
	if err != nil || text == "" || text == "body" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	calls := fake.snapshot()
	if len(calls) != 2 || calls[0].tool != "web_fetch" || calls[1].tool != "web_fetch_exa" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestHubSiteErrorDoesNotCoolServer(t *testing.T) {
	fake := &fakeCaller{errs: map[string]error{
		"firecrawl/firecrawl_scrape": errors.New("we do not support this site"),
	}}
	h, err := newHub(testConfig("firecrawl"), testProxy, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = h.Fetch(context.Background(), "https://example.com")
	_, _ = h.Fetch(context.Background(), "https://example.com")
	if calls := fake.snapshot(); len(calls) != 2 {
		t.Fatalf("site-level error should not cool the server: calls=%+v", calls)
	}
}

func TestHubUsesConfiguredKey(t *testing.T) {
	cfg := testConfig("parallel")
	cfg.Servers[0].APIKey = "test-key"
	fake := &fakeCaller{results: map[string]toolResult{
		"parallel": parallelToolResult("https://example.com", "P"),
	}}
	h, err := newHub(cfg, nil, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	calls := fake.snapshot()
	if len(calls) != 1 || calls[0].key != "test-key" {
		t.Fatalf("calls = %+v", calls)
	}
}

type fakeEngine struct {
	name    string
	results []core.SearchResult
	err     error
	calls   int
}

func (f *fakeEngine) Name() string { return f.name }
func (f *fakeEngine) Search(string) (string, error) {
	return "", errors.New("not used")
}
func (f *fakeEngine) SearchRaw(string) ([]core.SearchResult, error) {
	f.calls++
	return f.results, f.err
}
func (f *fakeEngine) MergeContent(string, []core.SearchResult) (string, error) {
	return "", errors.New("not used")
}

func TestHubFallbackEngine(t *testing.T) {
	fake := &fakeCaller{errs: map[string]error{
		"parallel": errors.New("connection refused"),
	}}
	eng := &fakeEngine{name: "so360", results: []core.SearchResult{{Title: "S", Url: "https://example.cn"}}}
	cfg := testConfig("parallel")
	cfg.FallbackEngines = []string{"so360"}
	h, err := newHub(cfg, nil, fake, map[string]core.SearchInf{"so360": eng})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.SearchRaw("q")
	if err != nil || len(got) != 1 || got[0].Engine != "so360" {
		t.Fatalf("results=%+v err=%v", got, err)
	}
	if eng.calls != 1 {
		t.Fatalf("engine calls = %d", eng.calls)
	}
}

func TestHubRotationEngine(t *testing.T) {
	fake := &fakeCaller{results: map[string]toolResult{
		"parallel": parallelToolResult("https://example.com/p", "P"),
	}}
	eng := &fakeEngine{name: "so360", results: []core.SearchResult{{Title: "S", Url: "https://example.cn"}}}
	cfg := testConfig("parallel")
	cfg.Strategy = "round-robin"
	cfg.RotationEngines = []string{"so360"}
	h, err := newHub(cfg, nil, fake, map[string]core.SearchInf{"so360": eng})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	if eng.calls != 1 {
		t.Fatalf("engine calls = %d, want 1 (second round-robin slot)", eng.calls)
	}
}

func TestBuildServerRejectsUnknownPreset(t *testing.T) {
	if _, err := buildServer(config.MCPHubServerConfig{Name: "custom"}); err == nil {
		t.Fatal("unknown server names must be rejected; request/response schemas are preset-specific")
	}
}

func TestBuildServerProxyDefaults(t *testing.T) {
	s, err := buildServer(config.MCPHubServerConfig{Name: "exa"})
	if err != nil || !s.requiresProxy {
		t.Fatalf("exa preset should require proxy: %+v err=%v", s, err)
	}
	disabled := false
	s, err = buildServer(config.MCPHubServerConfig{Name: "exa", RequiresProxy: &disabled})
	if err != nil || s.requiresProxy {
		t.Fatalf("explicit override should win: %+v err=%v", s, err)
	}
}

func TestHubFetchTruncatesRunes(t *testing.T) {
	fake := &fakeCaller{results: map[string]toolResult{
		"parallel/web_fetch": {Structured: map[string]any{"results": []any{
			map[string]any{"url": "https://example.com", "full_content": "你好世界"},
		}}},
	}}
	cfg := testConfig("parallel")
	cfg.MaxFetchChars = 3
	h, err := newHub(cfg, nil, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, err := h.Fetch(context.Background(), "https://example.com")
	if err != nil || !strings.HasPrefix(text, "你好世") || strings.Contains(text, "界") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestHubProxyGating(t *testing.T) {
	fake := &fakeCaller{results: map[string]toolResult{
		"parallel": parallelToolResult("https://example.com/p", "P"),
		"exa":      exaResult("E", "https://example.com/e"),
	}}
	cfg := testConfig("parallel", "exa")
	cfg.Strategy = "round-robin"
	h, err := newHub(cfg, nil, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.rotation[1].backend.Available() {
		t.Fatal("exa must be unavailable without a proxy")
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SearchRaw("q"); err != nil {
		t.Fatal(err)
	}
	calls := fake.snapshot()
	if len(calls) != 2 || calls[0].server != "parallel" || calls[1].server != "parallel" {
		t.Fatalf("proxy-required server must be skipped: %+v", calls)
	}

	proxyOn := ""
	resolver := func() string { return proxyOn }
	h, err = newHub(cfg, resolver, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.rotation[1].backend.Available() {
		t.Fatal("exa should start unavailable")
	}
	proxyOn = "http://127.0.0.1:7897"
	if !h.rotation[1].backend.Available() {
		t.Fatal("exa should become available when the resolver reports a proxy")
	}
}

func TestHubFetchSkipsEngines(t *testing.T) {
	disabled := false
	cfg := config.MCPHubConfig{
		Servers:         []config.MCPHubServerConfig{{Name: "parallel", Enabled: &disabled}},
		RotationEngines: []string{"so360"},
	}
	eng := &fakeEngine{name: "so360", results: []core.SearchResult{{Title: "S", Url: "https://example.cn"}}}
	h, err := newHub(cfg, nil, &fakeCaller{}, map[string]core.SearchInf{"so360": eng})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Fetch(context.Background(), "https://example.com"); err == nil {
		t.Fatal("engine backends must not serve fetch")
	}
	if eng.calls != 0 {
		t.Fatalf("engine calls = %d", eng.calls)
	}
}
