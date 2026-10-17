package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/llm"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCleanFetchReadsSavedResultWhenFetchingDisabledOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1})
	want := "BEGIN中文 END"
	searchapi.(*mockSearch).merged = want
	client := &http.Client{Timeout: 30 * time.Second}
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fixture","fetch_top_n":0}}}`)
	ref := resultRefFromNotice(t, jsonToolText(t, msg))
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "cleanfetch", "arguments": map[string]any{"result_ref": ref}}})
	code, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", string(payload))
	if code != http.StatusOK || msg["error"] != nil {
		t.Fatalf("saved body is not readable through cleanfetch: status=%d message=%v", code, msg)
	}
	var page ReadResultOutput
	if err := json.Unmarshal([]byte(jsonToolText(t, msg)), &page); err != nil {
		t.Fatal(err)
	}
	if page.Content != want || page.NextOffset != 11 || !page.EOF {
		t.Fatalf("cleanfetch changed the saved body: %+v", page)
	}
}

func TestCleanFetchModesAndTelemetryOverMCP(t *testing.T) {
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "usage.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	telemetry.SetDefault(store)
	t.Cleanup(func() { telemetry.SetDefault(nil); store.Close() })
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1})
	searchapi.(*mockSearch).merged = "PRIVATE_BODY_SENTINEL"
	client := &http.Client{Timeout: 30 * time.Second}
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fixture","fetch_top_n":0}}}`)
	ref := resultRefFromNotice(t, jsonToolText(t, msg))
	baseline, err := store.RecentFiltered(telemetry.EventFilter{Limit: 1})
	if err != nil || len(baseline) != 1 {
		t.Fatalf("missing search telemetry: %v %v", baseline, err)
	}
	_, _, listed := mcpHTTPPost(t, client, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := listed["result"].(map[string]any)["tools"].([]any)
	found := false
	for _, item := range tools {
		tool := item.(map[string]any)
		if tool["name"] == "read_result" {
			t.Fatal("standalone reader is still registered")
		}
		if tool["name"] == "cleanfetch" {
			found = true
		}
	}
	if !found {
		t.Fatal("cleanfetch must remain registered")
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"url": "https://example.com"}, "URL 抓取已关闭"},
		{map[string]any{"result_ref": ref, "url": "https://example.com"}, "不能与 url/urls 混用"},
		{map[string]any{"result_ref": ref, "urls": []string{"https://example.com"}}, "不能与 url/urls 混用"},
		{map[string]any{"url": "https://example.com", "offset": 0}, "仅用于 result_ref"},
		{map[string]any{"url": "https://example.com", "limit": 0}, "仅用于 result_ref"},
		{map[string]any{"result_ref": "https://example.com"}, "result_unavailable"},
		{map[string]any{"result_ref": ref}, ""},
	} {
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "cleanfetch", "arguments": tc.args}})
		code, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", string(payload))
		if code != http.StatusOK || msg["error"] != nil {
			t.Fatalf("call failed: %d %v", code, msg)
		}
		result := msg["result"].(map[string]any)
		if tc.want != "" {
			if result["isError"] != true || !strings.Contains(jsonToolText(t, msg), tc.want) {
				t.Fatalf("unexpected mode validation: %v", msg)
			}
		} else if result["isError"] == true {
			t.Fatalf("saved read failed: %v", msg)
		}
	}
	events, err := store.RecentFiltered(telemetry.EventFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	sawRead, sawFetch := false, false
	for _, event := range events {
		if event.ID <= baseline[0].ID {
			continue
		}
		if event.Kind == "provider" {
			t.Fatal("disabled fetch or saved read contacted an upstream")
		}
		if event.Tool == "cleanfetch" {
			sawRead = sawRead || event.Detail == "saved_result" && event.Success
			sawFetch = sawFetch || event.Detail == "url_fetch" && !event.Success
			if event.Detail == "saved_result" && (event.QueryHash != "" || event.AttemptChain != "") {
				t.Fatal("saved read recorded a query or upstream attempt")
			}
		}
	}
	raw, _ := json.Marshal(events)
	if !sawRead || !sawFetch || strings.Contains(string(raw), ref) || strings.Contains(string(raw), "PRIVATE_BODY_SENTINEL") {
		t.Fatalf("mode telemetry missing or private content leaked: %s", raw)
	}
}

func TestLongSearchReadableOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 100})
	want := "BEGIN\n" + strings.Repeat("fixed content\n", 100) + "END"
	searchapi.(*mockSearch).merged = want
	client := &http.Client{Timeout: 30 * time.Second}
	code, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fixture","fetch_top_n":0}}}`)
	if code != http.StatusOK || msg["error"] != nil {
		t.Fatalf("search: status=%d error=%v", code, msg["error"])
	}
	notice := jsonToolText(t, msg)
	ref := resultRefFromNotice(t, notice)
	if got := readAllResultOverMCP(t, srv.URL+"/mcp", ref, 37); got != want {
		t.Fatalf("MCP round trip lost content: got %d bytes, want %d", len(got), len(want))
	}
}

func resultRefFromNotice(t *testing.T, notice string) string {
	t.Helper()
	match := regexp.MustCompile("result_ref[^`]*`([^`]+)`").FindStringSubmatch(notice)
	if len(match) != 2 {
		t.Fatalf("saved body is unreachable through MCP: %s", notice)
	}
	return match[1]
}

func readAllResultOverMCP(t *testing.T, endpoint, ref string, limit int) string {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	var got strings.Builder
	offset := 0
	for i := 0; i < 1000; i++ {
		payload, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": i + 1, "method": "tools/call",
			"params": map[string]any{"name": "cleanfetch", "arguments": map[string]any{
				"result_ref": ref, "offset": offset, "limit": limit,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		code, _, msg := mcpHTTPPost(t, client, endpoint, string(payload))
		if code != http.StatusOK || msg["error"] != nil {
			t.Fatalf("cleanfetch: status=%d error=%v", code, msg["error"])
		}
		var page struct {
			Content    string `json:"content"`
			NextOffset int    `json:"next_offset"`
			EOF        bool   `json:"eof"`
		}
		text := jsonToolText(t, msg)
		if err := json.Unmarshal([]byte(text), &page); err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			t.Fatal(err)
		}
		structured := msg["result"].(map[string]any)["structuredContent"]
		if !reflect.DeepEqual(structured, decoded) {
			t.Fatalf("structured and text outputs differ: %v / %s", structured, text)
		}
		chars := utf8.RuneCountInString(page.Content)
		if chars > limit || page.NextOffset != offset+chars {
			t.Fatalf("invalid page bounds: offset=%d chars=%d page=%+v", offset, chars, page)
		}
		got.WriteString(page.Content)
		if page.EOF {
			return got.String()
		}
		if page.NextOffset <= offset {
			t.Fatal("cleanfetch did not advance")
		}
		offset = page.NextOffset
	}
	t.Fatal("cleanfetch did not reach EOF")
	return ""
}

func TestLongCleanFetchReadableOverMCP(t *testing.T) {
	var article strings.Builder
	article.WriteString("<html><head><title>Fixture article</title></head><body><article><h1>Fixture article</h1>")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&article, "<p>PARAGRAPH_%03d: This is fixed public fixture text for result delivery.</p>", i)
	}
	article.WriteString("</article></body></html>")
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == http.MethodConnect {
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n")
			rw.Flush()
			request, err := http.ReadRequest(rw.Reader)
			if err != nil {
				t.Error(err)
				return
			}
			request.Body.Close()
			fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", article.Len(), article.String())
			rw.Flush()
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, article.String())
	}))
	defer proxy.Close()
	conf := config.Config{MCPStateless: true, CleanFetch: config.CleanFetchConfig{Enabled: true, FileOutputDir: t.TempDir(), MaxInlineLines: 1, TimeoutSec: 3}}
	srv := newMCPHTTPTestServer(t, conf)
	webfetchLazyCfg = &config.Config{Proxy: config.ProxyConfig{Enabled: true, Endpoint: proxy.URL}}
	var err error
	webfetchInst, err = webfetch.NewFromConfig(conf.CleanFetch, config.PDFParserConfig{}, proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer webfetchInst.Close()
	code, _, msg := mcpHTTPPost(t, &http.Client{Timeout: 30 * time.Second}, srv.URL+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cleanfetch","arguments":{"url":"http://93.184.216.34/article"}}}`)
	if code != http.StatusOK || msg["error"] != nil {
		t.Fatalf("cleanfetch: %d %v", code, msg)
	}
	ref := resultRefFromNotice(t, jsonToolText(t, msg))
	before := requests.Load()
	body := readAllResultOverMCP(t, srv.URL+"/mcp", ref, 127)
	for i := 0; i < 100; i++ {
		if count := strings.Count(body, fmt.Sprintf("PARAGRAPH_%03d:", i)); count != 1 {
			t.Fatalf("paragraph %d appears %d times", i, count)
		}
	}
	if requests.Load() != before {
		t.Fatal("cleanfetch fetched the upstream again")
	}
}

func TestLongPDFReadableOverMCP(t *testing.T) {
	oldMaxPages := pdfMaxPages
	pdfMaxPages = 20
	t.Cleanup(func() { pdfMaxPages = oldMaxPages })
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(syntheticProxyPDFPages(3))
	}))
	defer proxy.Close()
	conf := config.Config{MCPStateless: true, PDFParser: config.PDFParserConfig{Enabled: true}, CleanFetch: config.CleanFetchConfig{FileOutputDir: t.TempDir(), MaxInlineChars: 1}}
	srv := newMCPHTTPTestServer(t, conf)
	webfetchLazyCfg = &config.Config{Proxy: config.ProxyConfig{Enabled: true, Endpoint: proxy.URL}}
	var err error
	webfetchInst, err = webfetch.NewFromConfig(conf.CleanFetch, conf.PDFParser, proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer webfetchInst.Close()
	code, _, msg := mcpHTTPPost(t, &http.Client{Timeout: 30 * time.Second}, srv.URL+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pdf_parser","arguments":{"path":"http://93.184.216.34/fixture.pdf","pages":"1-3"}}}`)
	if code != http.StatusOK || msg["error"] != nil {
		t.Fatalf("pdf_parser: %d %v", code, msg)
	}
	ref := resultRefFromNotice(t, jsonToolText(t, msg))
	before := requests.Load()
	body := readAllResultOverMCP(t, srv.URL+"/mcp", ref, 11)
	if strings.Count(body, "Proxy PDF text") != 3 {
		t.Fatalf("PDF text missing: %s", body)
	}
	if requests.Load() != before {
		t.Fatal("cleanfetch downloaded the PDF again")
	}
}

func TestSDKResultAcrossSessions(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		t.Run(fmt.Sprintf("stateless=%v", stateless), func(t *testing.T) {
			srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: stateless})
			setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{})
			want := "BEGIN\n" + strings.Repeat("中文🙂 e\u0301\n", 5000) + "END"
			searchapi.(*mockSearch).merged = want
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			connect := func() *mcp.ClientSession {
				t.Helper()
				cs, err := mcp.NewClient(&mcp.Implementation{Name: "isolated-result-client", Version: "1"}, nil).Connect(ctx,
					&mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true, MaxRetries: -1}, nil)
				if err != nil {
					t.Fatal(err)
				}
				return cs
			}
			first := connect()
			response, err := first.CallTool(ctx, &mcp.CallToolParams{Name: "smartsearch", Arguments: map[string]any{"query": "unicode fixture", "fetch_top_n": 0}})
			if err != nil || response.IsError {
				t.Fatalf("search: %v %v", response, err)
			}
			ref := resultRefFromNotice(t, response.Content[0].(*mcp.TextContent).Text)
			first.Close()
			second := connect()
			defer second.Close()
			tools, err := second.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, tool := range tools.Tools {
				if tool.Name == "cleanfetch" {
					found = tool.Annotations != nil && tool.Annotations.ReadOnlyHint && tool.InputSchema != nil
				}
			}
			if !found {
				t.Fatal("cleanfetch is not discoverable with read-only and input schema metadata")
			}
			var got strings.Builder
			for offset := 0; ; {
				response, err := second.CallTool(ctx, &mcp.CallToolParams{Name: "cleanfetch", Arguments: map[string]any{"result_ref": ref, "offset": offset, "limit": 4095, "project": "different-label"}})
				if err != nil || response.IsError {
					t.Fatalf("read: %v %v", response, err)
				}
				var page struct {
					Content string `json:"content"`
					Next    int    `json:"next_offset"`
					EOF     bool   `json:"eof"`
				}
				if err := json.Unmarshal([]byte(response.Content[0].(*mcp.TextContent).Text), &page); err != nil {
					t.Fatal(err)
				}
				if !utf8.ValidString(page.Content) || page.Next != offset+utf8.RuneCountInString(page.Content) {
					t.Fatal("Unicode page boundary is invalid")
				}
				got.WriteString(page.Content)
				if page.EOF {
					break
				}
				if page.Next <= offset {
					t.Fatal("SDK pagination did not advance")
				}
				offset = page.Next
			}
			if got.String() != want {
				t.Fatal("SDK round trip changed Unicode content")
			}
		})
	}
}

func TestReadResultRejectsUnsafeInputOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1})
	searchapi.(*mockSearch).merged = strings.Repeat("fixed", 1000)
	client := &http.Client{Timeout: 30 * time.Second}
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fixture","fetch_top_n":0}}}`)
	notice := jsonToolText(t, msg)
	ref := resultRefFromNotice(t, notice)
	_, rest, _ := strings.Cut(notice, "**文件路径**: `")
	path, _, _ := strings.Cut(rest, "`")
	for _, args := range []map[string]any{
		{"result_ref": "../../etc/passwd"}, {"result_ref": path}, {"result_ref": "result:unknown"},
		{"result_ref": ref, "offset": -1}, {"result_ref": ref, "limit": -1},
		{"result_ref": ref, "limit": 8193}, {"result_ref": ref, "offset": 100000},
	} {
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "cleanfetch", "arguments": args}})
		code, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", string(payload))
		if code != http.StatusOK || msg["result"].(map[string]any)["isError"] != true {
			t.Fatalf("unsafe arguments accepted: %v %v", args, msg)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(secret, []byte("PRIVATE_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "cleanfetch", "arguments": map[string]any{"result_ref": ref}}})
	_, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp", string(payload))
	if msg["result"].(map[string]any)["isError"] != true || strings.Contains(jsonToolText(t, msg), "PRIVATE_SENTINEL") {
		t.Fatal("replaced result followed a symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("PRIVATE_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp", string(payload))
	if msg["result"].(map[string]any)["isError"] != true {
		t.Fatal("replaced result file accepted")
	}
}

func TestCachedLongSearchReadableOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1})
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "result-delivery-test"
	want := "BEGIN_CACHE\n" + strings.Repeat("cached text ", 100) + "\nEND_CACHE"
	searchapi = &resultDeliverySearch{&mockSearch{name: "fixture", results: []search.SearchResult{{Title: "Cached article", Url: "https://example.com", Content: want}}}}
	client := &http.Client{Timeout: 30 * time.Second}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"cached fixture","fetch_top_n":0}}}`
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	firstRef := resultRefFromNotice(t, jsonToolText(t, msg))
	if got := readAllResultOverMCP(t, srv.URL+"/mcp", firstRef, 37); got != want {
		t.Fatal("first search did not return the complete fixture")
	}
	_, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	if !strings.Contains(jsonToolText(t, msg), "cache_age_seconds") {
		t.Fatal("second search did not hit cache")
	}
	secondRef := resultRefFromNotice(t, jsonToolText(t, msg))
	if secondRef == firstRef || readAllResultOverMCP(t, srv.URL+"/mcp", secondRef, 37) != want {
		t.Fatal("cached result did not produce a fresh usable reference")
	}
}

func TestLongSearchKeepsCandidatesOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1})
	searchapi.(*mockSearch).results = []search.SearchResult{
		{Title: "First candidate", Url: "https://example.com/first", Content: strings.Repeat("FIRST_SNIPPET ", 100)},
		{Title: "Second candidate", Url: "https://example.com/second", Content: strings.Repeat("SECOND_SNIPPET ", 100)},
	}
	searchapi.(*mockSearch).merged = strings.Repeat("FULL_BODY ", 100)
	_, _, msg := mcpHTTPPost(t, &http.Client{Timeout: 30 * time.Second}, srv.URL+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fixture","fetch_top_n":0}}}`)
	notice := jsonToolText(t, msg)
	for _, want := range []string{"First candidate", "https://example.com/first", "FIRST_SNIPPET", "Second candidate", "https://example.com/second", "SECOND_SNIPPET"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("long search hid candidate %q: %s", want, notice)
		}
	}
	if strings.Contains(notice, searchapi.(*mockSearch).merged) {
		t.Fatal("candidate preview includes the full body")
	}
}

type resultDeliverySearch struct{ *mockSearch }

func TestSearchBodyContainingFileLabelIsCacheableOverMCP(t *testing.T) {
	summary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"FIXED_\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"SUMMARY\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"FIXED_SUMMARY"}}]}`)
	}))
	defer summary.Close()
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true, LLM: config.LLMConfig{BaseURL: summary.URL, APIKey: "fixture", ModelId: "fixture"}})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: -1})
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "literal-file-label"
	want := "A documentation example contains **文件路径**: `example.md`, not a saved result."
	searchapi = &resultDeliverySearch{&mockSearch{name: "fixture", results: []search.SearchResult{{Title: "Documentation", Url: "https://example.com", Content: want}}}}
	client := &http.Client{Timeout: 30 * time.Second}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"literal label","intent":"","fetch_top_n":0}}}`
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	if !strings.Contains(jsonToolText(t, msg), want) {
		t.Fatal("first search lost ordinary document content")
	}
	_, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	text := jsonToolText(t, msg)
	if !strings.Contains(text, "cache_age_seconds") || strings.Count(text, want) != 1 {
		t.Fatalf("ordinary document was mistaken for a temporary file: %s", text)
	}
	summarizerInst = llm.NewSummarizer(summary.URL, "fixture", "fixture")
	request = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"summarized literal label","intent":"summarize","fetch_top_n":0}}}`
	_, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	text = jsonToolText(t, msg)
	if !strings.Contains(text, "FIXED_SUMMARY") || strings.Contains(text, want) {
		t.Fatalf("ordinary document was appended as a saved-file notice: %s", text)
	}
}

func TestSearchDoesNotReusePathOnlyCacheOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: -1})
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "legacy-file-notice"
	view, err := c.ForRequest(cacheNamespace)
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := `web:v2:{"Query":"legacy path","Months":3,"FetchTopN":0}`
	if err := view.Store(legacyKey, "", false, []search.SearchResult{{Content: "**文件路径**: `/deleted/fixture.md`"}}, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	searchapi = &resultDeliverySearch{&mockSearch{name: "fixture", results: []search.SearchResult{{Content: "CURRENT_DOCUMENT"}}}}
	_, _, msg := mcpHTTPPost(t, &http.Client{Timeout: 30 * time.Second}, srv.URL+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"legacy path","fetch_top_n":0}}}`)
	text := jsonToolText(t, msg)
	if !strings.Contains(text, "CURRENT_DOCUMENT") || strings.Contains(text, "/deleted/fixture.md") || strings.Contains(text, "cache_age_seconds") {
		t.Fatalf("search reused an old file notice: %s", text)
	}
}

func (s *resultDeliverySearch) MergeContent(_ string, results []search.SearchResult) (string, error) {
	var body strings.Builder
	for _, result := range results {
		body.WriteString(result.Content)
	}
	return body.String(), nil
}

// proxySyntheticPDF 以固定合成 PDF 应答取文请求。tls-client（go-webfetch 引擎）
// 的 connectDialer 对任何目标一律先发 CONNECT，Go 对未劫持的 CONNECT 应答后
// 是否继续按请求解析随版本而异（CI runner 上直接关连接，隧道内 GET 全部
// EOF），因此必须劫持连接实现真隧道：应答 200 后在隧道内读掉 GET 请求，
// 再以普通 HTTP 语义回写 PDF（带 Content-Length，客户端读满即完）。
func proxySyntheticPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		// 标准 http 传输的预检/下载（绝对 URI 转发）不走隧道
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(syntheticProxyPDFPages(1))
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	tmp := make([]byte, 4096)
	var seen []byte
	for !bytes.Contains(seen, []byte("\r\n\r\n")) {
		n, err := conn.Read(tmp)
		if n > 0 {
			seen = append(seen, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	conn.SetReadDeadline(time.Time{})
	pdf := syntheticProxyPDFPages(1)
	fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(pdf))
	conn.Write(pdf)
}

func TestFetchedSearchDoesNotReuseCleanedFileOverMCP(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxySyntheticPDF(w, r)
	}))
	defer proxy.Close()
	conf := config.Config{MCPStateless: true, PDFParser: config.PDFParserConfig{Enabled: true}, CleanFetch: config.CleanFetchConfig{FileOutputDir: t.TempDir(), MaxInlineChars: 1}}
	srv := newMCPHTTPTestServer(t, conf)
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: -1})
	webfetchLazyCfg = &config.Config{Proxy: config.ProxyConfig{Enabled: true, Endpoint: proxy.URL}}
	var err error
	webfetchInst, err = webfetch.NewFromConfig(conf.CleanFetch, conf.PDFParser, proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer webfetchInst.Close()
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "fetched-result-delivery"
	searchapi = &resultDeliverySearch{&mockSearch{name: "fixture", results: []search.SearchResult{{Title: "PDF article", Url: "http://93.184.216.34/fixture.pdf", Content: "snippet"}}}}
	client := &http.Client{Timeout: 30 * time.Second}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fetched fixture","fetch_top_n":1}}}`
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	notice := jsonToolText(t, msg)
	ref := resultRefFromNotice(t, notice)
	if body := readAllResultOverMCP(t, srv.URL+"/mcp", ref, 11); !strings.Contains(body, "Proxy PDF text") {
		t.Fatalf("first fetch has no PDF body: %s", body)
	}
	_, rest, _ := strings.Cut(notice, "**文件路径**: `")
	path, _, _ := strings.Cut(rest, "`")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, _, msg = mcpHTTPPost(t, client, srv.URL+"/mcp", request)
	freshRef := resultRefFromNotice(t, jsonToolText(t, msg))
	if freshRef == ref {
		t.Fatal("search reused a transient cached reference after file cleanup")
	}
	if body := readAllResultOverMCP(t, srv.URL+"/mcp", freshRef, 11); !strings.Contains(body, "Proxy PDF text") {
		t.Fatalf("new fetch has no PDF body: %s", body)
	}
}

func TestReadResultDefaultsEOFAuthOverMCP(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true, AuthToken: "fixture-token"})
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1})
	searchapi.(*mockSearch).merged = strings.Repeat("a", 4096) + "中文🙂 END"
	client := &http.Client{Timeout: 30 * time.Second}
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fixture","fetch_top_n":0}}}`, "Authorization", "Bearer fixture-token")
	ref := resultRefFromNotice(t, jsonToolText(t, msg))
	read := func(args map[string]any, headers ...string) (int, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "cleanfetch", "arguments": args}})
		code, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", string(payload), headers...)
		return code, msg
	}
	for _, headers := range [][]string{nil, {"Authorization", "Bearer wrong"}} {
		if code, _ := read(map[string]any{"result_ref": ref}, headers...); code != http.StatusUnauthorized {
			t.Fatalf("cleanfetch bypassed authentication: status %d", code)
		}
	}
	for _, args := range []map[string]any{{"result_ref": ref}, {"result_ref": ref, "limit": 0}} {
		code, msg := read(args, "X-API-Key", "fixture-token")
		if code != http.StatusOK {
			t.Fatalf("authenticated read: status %d", code)
		}
		var page ReadResultOutput
		if err := json.Unmarshal([]byte(jsonToolText(t, msg)), &page); err != nil {
			t.Fatal(err)
		}
		if page.Content != strings.Repeat("a", 4096) || page.NextOffset != 4096 || page.EOF {
			t.Fatal("default/zero limit did not return exactly 4096 characters")
		}
	}
	for _, offset := range []int{4096, 4103} {
		_, msg := read(map[string]any{"result_ref": ref, "offset": offset}, "Authorization", "Bearer fixture-token")
		var page ReadResultOutput
		if err := json.Unmarshal([]byte(jsonToolText(t, msg)), &page); err != nil {
			t.Fatal(err)
		}
		want := "中文🙂 END"
		if offset == 4103 {
			want = ""
		}
		if page.Content != want || page.NextOffset != 4103 || !page.EOF {
			t.Fatalf("unexpected EOF response: %+v", page)
		}
	}
}

func TestSummarizedFetchedSearchKeepsBodyReferenceOverMCP(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxySyntheticPDF(w, r)
	}))
	defer proxy.Close()
	var summaryCalls atomic.Int32
	summary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		summaryCalls.Add(1)
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"FIXED_SUMMARY\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"FIXED_SUMMARY"}}]}`)
	}))
	defer summary.Close()
	conf := config.Config{MCPStateless: true, LLM: config.LLMConfig{BaseURL: summary.URL, APIKey: "fixture", ModelId: "fixture"}, PDFParser: config.PDFParserConfig{Enabled: true}, CleanFetch: config.CleanFetchConfig{FileOutputDir: t.TempDir(), MaxInlineChars: 1}}
	srv := newMCPHTTPTestServer(t, conf)
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: -1})
	webfetchLazyCfg = &config.Config{Proxy: config.ProxyConfig{Enabled: true, Endpoint: proxy.URL}}
	var err error
	webfetchInst, err = webfetch.NewFromConfig(conf.CleanFetch, conf.PDFParser, proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer webfetchInst.Close()
	summarizerInst = llm.NewSummarizer(summary.URL, "fixture", "fixture")
	searchapi = &resultDeliverySearch{&mockSearch{name: "fixture", results: []search.SearchResult{{Title: "PDF article", Url: "http://93.184.216.34/fixture.pdf", Content: "snippet"}}}}
	_, _, msg := mcpHTTPPost(t, &http.Client{Timeout: 30 * time.Second}, srv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"fetched fixture","intent":"summarize","fetch_top_n":1}}}`)
	if summaryCalls.Load() == 0 {
		t.Fatal("search did not use the LLM summary path")
	}
	ref := resultRefFromNotice(t, jsonToolText(t, msg))
	if body := readAllResultOverMCP(t, srv.URL+"/mcp", ref, 11); !strings.Contains(body, "Proxy PDF text") {
		t.Fatalf("summarized search did not deliver the saved PDF body: %s", body)
	}
}
