package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"websearch/pkg/telemetry"
)

func TestHTTPProjectAttribution(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		t.Run(map[bool]string{false: "stateful", true: "stateless"}[stateless], func(t *testing.T) {
			store, err := telemetry.Open(filepath.Join(t.TempDir(), "usage.db"), 30)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			server := mcp.NewServer(&mcp.Implementation{Name: "attribution-test", Version: "1"}, nil)
			server.AddReceivingMiddleware(ClientAttributionMiddleware)
			addTool(server, &mcp.Tool{Name: "probe", Description: "synthetic attribution test"}, func(ctx context.Context, req *mcp.CallToolRequest, args projectParams) (*mcp.CallToolResult, any, error) {
				store.RecordContext(ctx, telemetry.Event{Kind: "tool", Tool: "probe", Success: true})
				store.RecordContext(ctx, telemetry.Event{Kind: "provider", Provider: "synthetic", Success: true})
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: stateless})
			srv := httptest.NewServer(handler)
			defer srv.Close()
			client := &http.Client{Timeout: 5 * time.Second}
			code, headers, _ := mcpHTTPPost(t, client, srv.URL, testInitializeReq, "X-WebSearch-Project", "Handshake-only")
			if code != 200 {
				t.Fatalf("initialize: %d", code)
			}
			sid := headers.Get("Mcp-Session-Id")
			cases := []struct{ arguments, header, want string }{
				{`{}`, "PZXM", "PZXM"},
				{`{"project":"FP"}`, "PZXM", "FP"},
				{`{"project":"声热制冷"}`, "", "声热制冷"},
				{`{"project":""}`, "PZXM", "PZXM"},
				{`{"project":"C:/private"}`, "PZXM", "PZXM"},
				{`{"project":"FP\n"}`, "PZXM", "PZXM"},
				{`{"project":".."}`, "PZXM", "PZXM"},
				{`{}`, "", ""},
				{`{}`, "C:/private", ""},
			}
			for _, tc := range cases {
				payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"probe","arguments":%s}}`, tc.arguments)
				code, _, msg := mcpHTTPPost(t, client, srv.URL, payload,
					"Mcp-Session-Id", sid, "User-Agent", "WorkBuddy/1", "X-WebSearch-Project", tc.header)
				if code != 200 || msg["error"] != nil || msg["result"].(map[string]any)["isError"] == true {
					t.Fatalf("call: %d %+v", code, msg)
				}
			}
			events, err := store.RecentFiltered(telemetry.EventFilter{Kind: "tool", Limit: 20})
			if err != nil || len(events) != len(cases) {
				t.Fatalf("events: %+v %v", events, err)
			}
			for i := range cases {
				want := cases[len(cases)-1-i].want
				if events[i].Project != want {
					t.Fatalf("project %d: %q want %q", i, events[i].Project, want)
				}
				if stateless && events[i].Client != "workbuddy" {
					t.Fatalf("stateless client: %q", events[i].Client)
				}
				if !stateless && events[i].Client != "test-client" {
					t.Fatalf("handshake client: %q", events[i].Client)
				}
			}
			providers, err := store.RecentFiltered(telemetry.EventFilter{Kind: "provider", Limit: 20})
			if err != nil || len(providers) != len(cases) {
				t.Fatalf("provider attribution: %+v %v", providers, err)
			}
			for i := range cases {
				if got, want := providers[i].Project, cases[len(cases)-1-i].want; got != want {
					t.Fatalf("provider project %d: %q want %q", i, got, want)
				}
			}
			code, _, msg := mcpHTTPPost(t, client, srv.URL, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"probe","arguments":{"project":42}}}`, "Mcp-Session-Id", sid)
			if code != 200 || msg["error"] == nil && msg["result"].(map[string]any)["isError"] != true {
				t.Fatalf("non-string project must fail schema validation: %d %+v", code, msg)
			}
		})
	}
}

func TestProjectAttributionArguments(t *testing.T) {
	for _, tc := range []struct{ name, arguments, header, want string }{
		{"argument-priority", `{"project":"FP","query":"unchanged"}`, "PZXM", "FP"},
		{"unicode", `{"project":"声热制冷"}`, "", "声热制冷"},
		{"unicode-escaped", `{"project":"\u58f0\u70ed\u5236\u51b7"}`, "", "声热制冷"},
		{"trim", `{"project":" FP "}`, "", "FP"},
		{"missing", `{}`, "", ""},
		{"empty", `{"project":""}`, "PZXM", "PZXM"},
		{"null", `{"project":null}`, "PZXM", "PZXM"},
		{"wrong-type", `{"project":42}`, "PZXM", "PZXM"},
		{"malformed-json", `{"project":`, "PZXM", "PZXM"},
		{"path", `{"project":"C:/private"}`, "PZXM", "PZXM"},
		{"backslash", `{"project":"C:\\private"}`, "PZXM", "PZXM"},
		{"url", `{"project":"https://private"}`, "", ""},
		{"control", `{"project":"FP\n"}`, "PZXM", "PZXM"},
		{"dot", `{"project":"."}`, "", ""},
		{"parent", `{"project":" .. "}`, "", ""},
		{"length", `{"project":"` + strings.Repeat("中", 65) + `"}`, "PZXM", "PZXM"},
		{"invalid-header", `{}`, "PZXM\n", ""},
		{"dot-header", `{}`, ".", ""},
		{"parent-header", `{}`, "..", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Name: "probe", Arguments: json.RawMessage(tc.arguments)},
				Extra:  &mcp.RequestExtra{Header: http.Header{}},
			}
			req.Extra.Header.Set("X-WebSearch-Project", tc.header)
			handler := ClientAttributionMiddleware(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				if got := telemetry.ProjectFromContext(ctx); got != tc.want {
					t.Errorf("project = %q, want %q", got, tc.want)
				}
				return &mcp.CallToolResult{}, nil
			})
			if _, err := handler(telemetry.WithProject(context.Background(), "previous-call"), "tools/call", req); err != nil {
				t.Fatal(err)
			}
			if string(req.Params.Arguments) != tc.arguments {
				t.Fatal("attribution changed business arguments")
			}
		})
	}
}

func TestToolProjectSchema(t *testing.T) {
	for _, withIntent := range []bool{false, true} {
		t.Run(fmt.Sprintf("intent-%t", withIntent), func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "schema-test", Version: "1"}, nil)
			if withIntent {
				addTool(server, &mcp.Tool{Name: "smartsearch"}, WebSearchWithIntent)
			} else {
				addTool(server, &mcp.Tool{Name: "smartsearch"}, WebSearchNoIntent)
			}
			addTool(server, &mcp.Tool{Name: "academicsearch"}, AcademicSearchHandler)
			addTool(server, &mcp.Tool{Name: "cleanfetch"}, CleanFetch)
			addTool(server, &mcp.Tool{Name: "pdf_parser"}, PDFParserHandler)
			addTool(server, &mcp.Tool{Name: "file_search"}, FileSearch)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			t1, t2 := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, t1, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "schema-client", Version: "1"}, nil).Connect(ctx, t2, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			tools, err := cs.ListTools(ctx, nil)
			if err != nil || len(tools.Tools) != 5 {
				t.Fatalf("tools: %+v %v", tools, err)
			}
			for _, tool := range tools.Tools {
				raw, err := json.Marshal(tool.InputSchema)
				if err != nil {
					t.Fatal(err)
				}
				var schema struct {
					Properties map[string]struct {
						Type        any
						Description string
					}
					Required []string
				}
				if err := json.Unmarshal(raw, &schema); err != nil {
					t.Fatal(err)
				}
				project, ok := schema.Properties["project"]
				if !ok || project.Type != "string" || slices.Contains(schema.Required, "project") {
					t.Errorf("%s project must be optional string: %s", tool.Name, raw)
				}
				for _, rule := range []string{"已明确", "未知则省略", "不发送路径", "不按查询", "不要求用户提供", "仅用于统计", "支持中文"} {
					if !strings.Contains(project.Description, rule) {
						t.Errorf("%s project description missing %q", tool.Name, rule)
					}
				}
			}
		})
	}
}

func TestClientAttributionRequestIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, client, explicit, ua, want string
	}{
		{"standard-meta", "WorkBuddy", "", "node", "workbuddy"},
		{"generic-known-ua", "websearch", "", "WorkBuddy/1", "workbuddy"},
		{"generic-plain-ua", "websearch", "", "node", "websearch"},
		{"explicit-generic-priority", "codex-mcp-client", "websearch", "WorkBuddy/1", "websearch"},
		{"explicit-generic-host", "websearch", "WorkBuddy", "node", "workbuddy"},
		{"explicit-priority", "codex-mcp-client", "workbuddy", "Cursor/1", "workbuddy"},
		{"invalid-path", "codex-mcp-client", "C:/private", "node", "codex-mcp-client"},
		{"invalid-url", "codex-mcp-client", "https://private", "node", "codex-mcp-client"},
		{"invalid-control", "codex-mcp-client", "workbuddy\nprivate", "node", "codex-mcp-client"},
		{"invalid-trailing-control", "codex-mcp-client", "workbuddy\n", "node", "codex-mcp-client"},
		{"invalid-relative-path", "codex-mcp-client", "..", "node", "codex-mcp-client"},
		{"invalid-backslash-path", "codex-mcp-client", `C:\private`, "node", "codex-mcp-client"},
		{"invalid-length", "codex-mcp-client", "abcdefghijklmnopqrstuvwxyzabcdefghi", "node", "codex-mcp-client"},
		{"unknown-ua-fallback", "", "", "WorkBuddy/1", "workbuddy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := &mcp.CallToolParamsRaw{Name: "probe", Arguments: json.RawMessage(`{}`)}
			if tc.client != "" {
				params.SetMeta(map[string]any{mcp.MetaKeyClientInfo: map[string]any{"name": tc.client, "version": "1"}})
			}
			req := &mcp.CallToolRequest{Params: params, Extra: &mcp.RequestExtra{Header: http.Header{}}}
			req.Extra.Header.Set("User-Agent", tc.ua)
			req.Extra.Header.Set("X-WebSearch-Client", tc.explicit)
			handler := ClientAttributionMiddleware(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				if got := telemetry.ClientFromContext(ctx); got != tc.want {
					t.Fatalf("client = %q, want %q", got, tc.want)
				}
				return &mcp.CallToolResult{}, nil
			})
			if _, err := handler(context.Background(), "tools/call", req); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPClientAttribution(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		t.Run(map[bool]string{false: "stateful", true: "stateless"}[stateless], func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "attribution-test", Version: "1"}, nil)
			server.AddReceivingMiddleware(ClientAttributionMiddleware)
			addTool(server, &mcp.Tool{Name: "probe", Description: "synthetic attribution test"}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: telemetry.ClientFromContext(ctx)}}}, nil, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: stateless})
			srv := httptest.NewServer(handler)
			defer srv.Close()
			client := &http.Client{Timeout: 5 * time.Second}
			code, headers, _ := mcpHTTPPost(t, client, srv.URL, testInitializeReq)
			if code != 200 {
				t.Fatalf("initialize: %d", code)
			}
			for _, explicit := range []string{"", "WorkBuddy", "C:/private"} {
				payload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"probe","arguments":{},"_meta":{"io.modelcontextprotocol/clientInfo":{"name":"codex-mcp-client","version":"1"}}}}`
				protocol := "2025-06-18"
				if stateless {
					protocol = "2026-07-28"
					payload = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"probe","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"codex-mcp-client","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
				}
				code, _, msg := mcpHTTPPost(t, client, srv.URL, payload,
					"Mcp-Session-Id", headers.Get("Mcp-Session-Id"), "MCP-Protocol-Version", protocol, "MCP-Method", "tools/call", "MCP-Name", "probe", "User-Agent", "node", "X-WebSearch-Client", explicit)
				if code != 200 || msg["error"] != nil {
					t.Fatalf("call: %d %+v", code, msg)
				}
				want := "codex-mcp-client"
				if explicit == "WorkBuddy" {
					want = "workbuddy"
				}
				got := msg["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
				if got != want {
					t.Fatalf("client = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestProjectAttributionStdioConcurrent(t *testing.T) {
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "usage.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "attribution-test", Version: "1"}, nil)
	server.AddReceivingMiddleware(ClientAttributionMiddleware)
	mcp.AddTool(server, &mcp.Tool{Name: "probe", Description: "synthetic attribution test"}, func(ctx context.Context, req *mcp.CallToolRequest, args projectParams) (*mcp.CallToolResult, any, error) {
		if req.Session.ID() != "" {
			return nil, nil, fmt.Errorf("stdio session unexpectedly has ID %q", req.Session.ID())
		}
		store.RecordContext(ctx, telemetry.Event{Kind: "tool", Tool: "probe", Success: true})
		store.RecordContext(ctx, telemetry.Event{Kind: "provider", Provider: "synthetic", Success: true})
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: telemetry.ClientFromContext(ctx) + "|" + telemetry.ProjectFromContext(ctx)}}}, nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var calls sync.WaitGroup
	for _, name := range []string{"workbuddy", "codex-mcp-client"} {
		serverIn, clientOut := io.Pipe()
		clientIn, serverOut := io.Pipe()
		ss, err := server.Connect(ctx, &mcp.IOTransport{Reader: serverIn, Writer: serverOut}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ss.Close()
		defer clientIn.Close()
		defer clientOut.Close()
		cs, err := mcp.NewClient(&mcp.Implementation{Name: name, Version: "1"}, nil).Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cs.Close()
		for range 4 {
			for _, tc := range []struct{ project, want string }{{name + "-project", name + "-project"}, {"声热制冷", "声热制冷"}, {"", ""}, {"C:/private", ""}} {
				calls.Add(1)
				go func() {
					defer calls.Done()
					args := map[string]any{}
					if tc.project != "" {
						args["project"] = tc.project
					}
					result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "probe", Arguments: args})
					if err != nil || result.IsError || len(result.Content) != 1 {
						t.Errorf("stdio call: %+v %v", result, err)
						return
					}
					if got, want := result.Content[0].(*mcp.TextContent).Text, name+"|"+tc.want; got != want {
						t.Errorf("stdio attribution = %q, want %q", got, want)
					}
				}()
			}
		}
	}
	calls.Wait()
	for _, kind := range []string{"tool", "provider"} {
		events, err := store.RecentFiltered(telemetry.EventFilter{Kind: kind, Limit: 100})
		if err != nil || len(events) != 32 {
			t.Fatalf("%s events: %d %v", kind, len(events), err)
		}
		counts := map[string]int{}
		for _, event := range events {
			counts[event.Client+"|"+event.Project]++
		}
		for _, name := range []string{"workbuddy", "codex-mcp-client"} {
			for project, want := range map[string]int{name + "-project": 4, "声热制冷": 4, "": 8} {
				if got := counts[name+"|"+project]; got != want {
					t.Errorf("%s %s|%s count = %d, want %d", kind, name, project, got, want)
				}
			}
		}
	}
}

func TestClientAttributionStdioConcurrent(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "attribution-test", Version: "1"}, nil)
	server.AddReceivingMiddleware(ClientAttributionMiddleware)
	mcp.AddTool(server, &mcp.Tool{Name: "probe", Description: "synthetic attribution test"}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		if req.Session.ID() != "" {
			return nil, nil, fmt.Errorf("stdio-equivalent session unexpectedly has ID %q", req.Session.ID())
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: telemetry.ClientFromContext(ctx)}}}, nil, nil
	})
	var wg sync.WaitGroup
	for _, name := range []string{"workbuddy", "codex-mcp-client"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			serverIn, clientOut := io.Pipe()
			clientIn, serverOut := io.Pipe()
			ss, err := server.Connect(ctx, &mcp.IOTransport{Reader: serverIn, Writer: serverOut}, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer ss.Close()
			defer clientIn.Close()
			defer clientOut.Close()
			decoder := json.NewDecoder(clientIn)
			if _, err := fmt.Fprintf(clientOut, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":%q,"version":"1"}}}`+"\n", name); err != nil {
				t.Error(err)
				return
			}
			var initialized map[string]any
			if err := decoder.Decode(&initialized); err != nil || initialized["error"] != nil {
				t.Errorf("initialize: %v %+v", err, initialized)
				return
			}
			for range 3 {
				if _, err := fmt.Fprintln(clientOut, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"probe","arguments":{}}}`); err != nil {
					t.Error(err)
					return
				}
				var response struct {
					Result struct {
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"result"`
				}
				if err := decoder.Decode(&response); err != nil {
					t.Error(err)
					return
				}
				if len(response.Result.Content) == 0 {
					t.Errorf("empty stdio response: %+v", response)
					return
				}
				if got := response.Result.Content[0].Text; got != name {
					t.Errorf("stdio client = %q, want %q", got, name)
				}
			}
		}()
	}
	wg.Wait()
}
