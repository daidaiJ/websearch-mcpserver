package mcphub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"websearch/pkg/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHeaderTransportUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name, ua, want string
	}{
		{"fallback", "", defaultUserAgent},
		{"passthrough", "WorkBuddy/1", "WorkBuddy/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			transport := &headerTransport{
				base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					got = r.Header.Get("User-Agent")
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
				}),
				userAgent: tc.ua,
			}
			req, err := http.NewRequest(http.MethodPost, "https://example.com/mcp", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if got != tc.want {
				t.Fatalf("User-Agent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMCPCallerForwardsClientUserAgent(t *testing.T) {
	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "ua-test", Version: "1"}, nil)
	mcp.AddTool(mcpSrv, &mcp.Tool{Name: "probe", Description: "synthetic UA test"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpSrv }, nil)

	var (
		mu  sync.Mutex
		uas []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uas = append(uas, r.Header.Get("User-Agent"))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx := telemetry.WithClientUserAgent(t.Context(), "WorkBuddy/1")
	caller := &mcpCaller{base: http.DefaultTransport}
	if _, err := caller.Call(ctx, &server{name: "probe", url: srv.URL}, "probe", map[string]any{}, 5*time.Second, ""); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(uas) == 0 {
		t.Fatal("no upstream request observed")
	}
	for _, ua := range uas {
		if ua != "WorkBuddy/1" {
			t.Fatalf("upstream User-Agent = %q, want passthrough", ua)
		}
	}
}

func TestMCPCallerFallsBackWithoutClientUserAgent(t *testing.T) {
	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "ua-test", Version: "1"}, nil)
	mcp.AddTool(mcpSrv, &mcp.Tool{Name: "probe", Description: "synthetic UA test"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpSrv }, nil)

	var (
		mu  sync.Mutex
		uas []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uas = append(uas, r.Header.Get("User-Agent"))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	caller := &mcpCaller{base: http.DefaultTransport}
	if _, err := caller.Call(context.Background(), &server{name: "probe", url: srv.URL}, "probe", map[string]any{}, 5*time.Second, ""); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(uas) == 0 {
		t.Fatal("no upstream request observed")
	}
	for _, ua := range uas {
		if ua != defaultUserAgent {
			t.Fatalf("upstream User-Agent = %q, want default", ua)
		}
	}
}
