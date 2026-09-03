package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/webfetch"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockSearch struct {
	name    string
	results []search.SearchResult
	merged  string
	err     error
	lastQ   string
}

func (m *mockSearch) Name() string { return m.name }
func (m *mockSearch) Search(query string) (string, error) {
	return m.merged, m.err
}
func (m *mockSearch) SearchRaw(query string) ([]search.SearchResult, error) {
	m.lastQ = query
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}
func (m *mockSearch) MergeContent(query string, results []search.SearchResult) (string, error) {
	if m.merged != "" {
		return m.merged, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "query=%s", query)
	for _, r := range results {
		fmt.Fprintf(&b, "\n%s %s", r.Title, r.Url)
	}
	return b.String(), nil
}

type mockAcademic struct {
	engines []string
	results []search.SearchResult
	err     error
	lastQ   string
	lastOpt search.AcademicSearchOptions
}

func (m *mockAcademic) AcademicEngines() []string { return m.engines }
func (m *mockAcademic) SearchAcademicRaw(query string, opts ...search.AcademicSearchOptions) (search.AcademicSearchResult, error) {
	m.lastQ = query
	if len(opts) > 0 {
		m.lastOpt = opts[0]
	}
	if m.err != nil {
		return search.AcademicSearchResult{}, m.err
	}
	return search.AcademicSearchResult{Results: m.results}, nil
}

func initTestLogger() {
	log.NewLoggerTo(io.Discard, "", config.LogConfig{})
}

func restoreGlobals(t *testing.T) {
	t.Helper()
	oldSearch := searchapi
	oldAcad := academicSearcher
	oldWF := webfetchInst
	oldCache := cacheInst
	oldSum := summarizerInst
	oldJina := jinaInst
	oldFallback := fallbackSearch
	oldSmart := smartSearchConf
	t.Cleanup(func() {
		searchapi = oldSearch
		academicSearcher = oldAcad
		webfetchInst = oldWF
		cacheInst = oldCache
		summarizerInst = oldSum
		jinaInst = oldJina
		fallbackSearch = oldFallback
		smartSearchConf = oldSmart
	})
}

func listToolNames(t *testing.T, conf config.Config) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t1, t2 := mcp.NewInMemoryTransports()
	server := NewMCPServer(conf, nil)
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func toolByName(t *testing.T, conf config.Config, name string) *mcp.Tool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := NewMCPServer(conf, nil).Connect(ctx, t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func TestNewMCPServer_RegisterTools(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)

	tests := []struct {
		name    string
		setup   func()
		conf    config.Config
		want    []string
		wantNot []string
	}{
		{
			name: "bing on, academic mock, optional off",
			setup: func() {
				academicSearcher = &mockAcademic{engines: []string{"arxiv"}}
				webfetchInst = nil
			},
			conf: config.Config{
				Bing:     config.BingConfig{Enabled: true},
				Academic: config.AcademicConfig{Enabled: true},
			},
			want:    []string{"academicsearch", "smartsearch"},
			wantNot: []string{"cleanfetch", "pdf_parser"},
		},
		{
			name: "academic enabled but searcher nil",
			setup: func() {
				academicSearcher = nil
			},
			conf: config.Config{
				Bing:     config.BingConfig{Enabled: true},
				Academic: config.AcademicConfig{Enabled: true},
			},
			want:    []string{"smartsearch"},
			wantNot: []string{"academicsearch"},
		},
		{
			name: "bing off, optional on with dummy webfetch",
			setup: func() {
				academicSearcher = nil
				webfetchInst = &webfetch.Fetcher{}
			},
			conf: config.Config{
				Bing:       config.BingConfig{Enabled: false},
				CleanFetch: config.CleanFetchConfig{Enabled: true},
				PDFParser:  config.PDFParserConfig{Enabled: true},
			},
			want:    []string{"cleanfetch", "pdf_parser"},
			wantNot: []string{"smartsearch", "academicsearch"},
		},
		{
			name: "pdf enabled but webfetch nil",
			setup: func() {
				webfetchInst = nil
			},
			conf: config.Config{
				PDFParser: config.PDFParserConfig{Enabled: true},
			},
			wantNot: []string{"pdf_parser"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			academicSearcher = nil
			webfetchInst = nil
			searchapi = nil
			if tt.setup != nil {
				tt.setup()
			}
			names := listToolNames(t, tt.conf)
			for _, w := range tt.want {
				if !slices.Contains(names, w) {
					t.Errorf("expected %s, got %v", w, names)
				}
			}
			for _, n := range tt.wantNot {
				if slices.Contains(names, n) {
					t.Errorf("did not expect %s, got %v", n, names)
				}
			}
		})
	}
}

func TestNewMCPServer_SmartsearchIntentDescription(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	academicSearcher = nil
	webfetchInst = nil

	noLLM := toolByName(t, config.Config{Bing: config.BingConfig{Enabled: true}}, "smartsearch")
	if strings.Contains(noLLM.Description, "intent") {
		t.Errorf("no-LLM description should not mention intent, got %q", noLLM.Description)
	}

	withLLM := toolByName(t, config.Config{
		Bing: config.BingConfig{Enabled: true},
		LLM:  config.LLMConfig{BaseURL: "http://x", APIKey: "k", ModelId: "m"},
	}, "smartsearch")
	if !strings.Contains(withLLM.Description, "intent") {
		t.Errorf("LLM description should mention intent, got %q", withLLM.Description)
	}
}

func TestBuildAcademicToolDescription_MockEngines(t *testing.T) {
	restoreGlobals(t)
	academicSearcher = &mockAcademic{engines: []string{"arxiv", "custom_engine"}}
	desc := buildAcademicToolDescription()
	if !strings.Contains(desc, "arxiv") {
		t.Errorf("missing arxiv: %s", desc)
	}
	if !strings.Contains(desc, "CS/物理/数学") {
		t.Errorf("missing known engine desc: %s", desc)
	}
	if !strings.Contains(desc, "custom_engine") {
		t.Errorf("unknown engine should still be listed: %s", desc)
	}
}

func TestRunWithTransport_CallToolsMocked(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)

	ms := &mockSearch{
		name:    "mock",
		results: []search.SearchResult{{Title: "Go generics", Url: "https://example.com/go", Content: "body"}},
		merged:  "MERGED:Go generics",
	}
	ma := &mockAcademic{
		engines: []string{"arxiv"},
		results: []search.SearchResult{{Title: "Attention paper", Url: "https://arxiv.org/abs/1", Type: "paper"}},
	}
	searchapi = ms
	academicSearcher = ma
	webfetchInst = nil
	cacheInst = nil
	summarizerInst = nil
	fallbackSearch = nil

	conf := config.Config{
		Bing:     config.BingConfig{Enabled: true},
		Academic: config.AcademicConfig{Enabled: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	t1, t2 := mcp.NewInMemoryTransports()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runWithTransport(ctx, conf, t1, slog.New(slog.DiscardHandler))
	}()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "smartsearch") || !slices.Contains(names, "academicsearch") {
		t.Fatalf("tools = %v", names)
	}

	web, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "smartsearch",
		Arguments: map[string]any{"query": "Go generics", "time_range": 1},
	})
	if err != nil {
		t.Fatalf("smartsearch: %v", err)
	}
	text := toolText(t, web)
	if !strings.Contains(text, "MERGED:Go generics") {
		t.Errorf("smartsearch result = %q", text)
	}
	if ms.lastQ != "Go generics" {
		t.Errorf("mock SearchRaw query = %q", ms.lastQ)
	}

	acad, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "academicsearch",
		Arguments: map[string]any{
			"query":      "transformer",
			"engines":    []string{"arxiv"},
			"time_range": "year",
			"page":       2,
		},
	})
	if err != nil {
		t.Fatalf("academicsearch: %v", err)
	}
	if ma.lastQ != "transformer" {
		t.Errorf("academic query = %q", ma.lastQ)
	}
	if ma.lastOpt.Page != 2 || ma.lastOpt.TimeRange != "year" {
		t.Errorf("academic opts = %+v", ma.lastOpt)
	}
	if !strings.Contains(toolText(t, acad), "Attention paper") && !strings.Contains(toolText(t, acad), "MERGED") {
		t.Errorf("academic result = %q", toolText(t, acad))
	}

	_ = cs.Close()
	cancel()
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Error("runWithTransport did not return after cancel")
	}
}

func TestRunWithTransport_SearchError(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	searchapi = &mockSearch{name: "mock", err: fmt.Errorf("engine down")}
	academicSearcher = nil
	fallbackSearch = nil
	cacheInst = nil

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t1, t2 := mcp.NewInMemoryTransports()
	go func() {
		_ = runWithTransport(ctx, config.Config{Bing: config.BingConfig{Enabled: true}}, t1, slog.New(slog.DiscardHandler))
	}()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "smartsearch",
		Arguments: map[string]any{"query": "x"},
	})
	if err != nil {
		return
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error, got err=%v res=%v", err, res)
	}
}

func TestRegisterRouter_MountsMCP(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	academicSearcher = nil
	webfetchInst = nil

	mux := http.NewServeMux()
	RegisterRouter(mux, config.Config{Bing: config.BingConfig{Enabled: true}})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatal("/mcp was not registered")
	}
}

func TestAuthMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no token configured -> pass through", func(t *testing.T) {
		h := AuthMiddleware(config.Config{}, ok)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200", w.Code)
		}
	})

	t.Run("missing header -> 401", func(t *testing.T) {
		h := AuthMiddleware(config.Config{AuthToken: "secret"}, ok)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, want 401", w.Code)
		}
	})

	t.Run("wrong token -> 401", func(t *testing.T) {
		h := AuthMiddleware(config.Config{AuthToken: "secret"}, ok)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, want 401", w.Code)
		}
	})

	t.Run("Bearer token -> 200", func(t *testing.T) {
		h := AuthMiddleware(config.Config{AuthToken: "secret"}, ok)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200", w.Code)
		}
	})

	t.Run("X-API-Key -> 200", func(t *testing.T) {
		h := AuthMiddleware(config.Config{AuthToken: "secret"}, ok)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-API-Key", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200", w.Code)
		}
	})
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type %T", res.Content[0])
	}
	return tc.Text
}

// ── RegisterRouter Streamable HTTP 无状态/有状态模式测试 ──
// 通过 httptest 起 /mcp 端点，mock 搜索引擎注入全局实例，
// 用原始 JSON-RPC over HTTP 验证两种模式的协议行为差异。

// newMCPHTTPTestServer 装配 mock 引擎并启动带 /mcp 路由的测试服务。
func newMCPHTTPTestServer(t *testing.T, conf config.Config) *httptest.Server {
	t.Helper()
	initTestLogger()
	restoreGlobals(t)
	searchapi = &mockSearch{
		name:    "mock",
		results: []search.SearchResult{{Title: "Go generics", Url: "https://example.com/go", Content: "body"}},
		merged:  "MERGED:mock",
	}
	academicSearcher = &mockAcademic{engines: []string{"arxiv"}}
	webfetchInst = nil
	cacheInst = nil
	summarizerInst = nil
	fallbackSearch = nil

	mux := http.NewServeMux()
	RegisterRouter(mux, conf)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// mcpHTTPPost 发送 JSON-RPC POST，返回状态码、响应头与解析出的 JSON-RPC 消息。
// 兼容 application/json 与 text/event-stream（SSE，取最后一个 data: 行）两种响应格式。
func mcpHTTPPost(t *testing.T, client *http.Client, url, payload string, extraHdr ...string) (int, http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(extraHdr); i += 2 {
		req.Header.Set(extraHdr[i], extraHdr[i+1])
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, resp.Header, nil
	}
	var msg map[string]any
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(body), "\n") {
			data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
			if !ok {
				continue
			}
			var m map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &m) == nil {
				msg = m
			}
		}
		if msg == nil {
			t.Fatalf("no JSON-RPC message in SSE body: %q", body)
		}
	} else {
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Fatalf("parse JSON response %q: %v", body, err)
		}
	}
	return resp.StatusCode, resp.Header, msg
}

// jsonToolText 从 JSON-RPC 响应 result 中提取首个 text content。
func jsonToolText(t *testing.T, msg map[string]any) string {
	t.Helper()
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", msg)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %v", result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

const testInitializeReq = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"0"}}}`

func TestRegisterRouter_StatelessMode(t *testing.T) {
	initTestLogger()
	srv := newMCPHTTPTestServer(t, config.Config{
		Bing:         config.BingConfig{Enabled: true},
		MCPStateless: true,
	})
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("tools/list without initialize succeeds", func(t *testing.T) {
		code, header, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if got := header.Get("Mcp-Session-Id"); got != "" {
			t.Errorf("stateless server should not mint session id, got %q", got)
		}
		if _, ok := msg["result"]; !ok {
			t.Fatalf("expected result, got %v", msg)
		}
	})

	t.Run("GET returns 405 with Allow POST", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/mcp")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", resp.StatusCode)
		}
		if allow := resp.Header.Get("Allow"); allow != "POST" {
			t.Errorf("Allow = %q, want POST", allow)
		}
	})

	t.Run("initialize response has no session id", func(t *testing.T) {
		code, header, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", testInitializeReq)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if got := header.Get("Mcp-Session-Id"); got != "" {
			t.Errorf("stateless initialize should not set session id, got %q", got)
		}
		if _, ok := msg["result"]; !ok {
			t.Fatalf("expected initialize result, got %v", msg)
		}
	})

	t.Run("tools/call end-to-end with mock engine", func(t *testing.T) {
		code, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"golang"}}}`)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if errMsg, ok := msg["error"]; ok {
			t.Fatalf("unexpected JSON-RPC error: %v", errMsg)
		}
		if text := jsonToolText(t, msg); !strings.Contains(text, "MERGED:mock") {
			t.Errorf("smartsearch result = %q, want containing MERGED:mock", text)
		}
	})
}

func TestRegisterRouter_StatefulMode(t *testing.T) {
	initTestLogger()
	srv := newMCPHTTPTestServer(t, config.Config{Bing: config.BingConfig{Enabled: true}})
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("GET without session rejected", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/mcp")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (GET requires session in stateful mode)", resp.StatusCode)
		}
	})

	t.Run("initialize mints session id", func(t *testing.T) {
		code, header, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", testInitializeReq)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if _, ok := msg["result"]; !ok {
			t.Fatalf("expected initialize result, got %v", msg)
		}
		sessionID := header.Get("Mcp-Session-Id")
		if sessionID == "" {
			t.Fatal("stateful initialize should mint Mcp-Session-Id")
		}

		// 携带会话 ID 的后续请求可用
		code, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp",
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, "Mcp-Session-Id", sessionID)
		if code != http.StatusOK {
			t.Fatalf("tools/list with session status = %d, want 200", code)
		}
		if _, ok := msg["result"]; !ok {
			t.Fatalf("expected tools/list result, got %v", msg)
		}

		// 未知会话 ID 被拒绝
		code, _, _ = mcpHTTPPost(t, client, srv.URL+"/mcp",
			`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, "Mcp-Session-Id", "bogus-session")
		if code != http.StatusNotFound {
			t.Fatalf("tools/list with bogus session status = %d, want 404", code)
		}
	})
}
