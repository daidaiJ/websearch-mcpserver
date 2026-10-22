package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search/provider/mcphub"
)

type fakeHubFetcher struct {
	text  string
	err   error
	calls int
}

func (f *fakeHubFetcher) Fetch(context.Context, string) (string, error) {
	f.calls++
	return f.text, f.err
}

func TestFetchPageWithFallback(t *testing.T) {
	localOK := func(context.Context, string) (string, error) { return "local", nil }
	localErr := func(context.Context, string) (string, error) { return "", errors.New("local failed") }
	hubOK := &fakeHubFetcher{text: "hub"}
	hubErr := &fakeHubFetcher{err: errors.New("hub failed")}

	tests := []struct {
		name      string
		mode      string
		hub       *fakeHubFetcher
		local     func(context.Context, string) (string, error)
		want      string
		wantErr   string
		wantCalls int
	}{
		{"off uses local", config.MCPHubFetchOff, hubOK, localOK, "local", "", 0},
		{"prefer hub", config.MCPHubFetchPrefer, hubOK, localErr, "hub", "", 1},
		{"prefer degrades", config.MCPHubFetchPrefer, hubErr, localOK, "local", "", 1},
		{"prefer both fail", config.MCPHubFetchPrefer, hubErr, localErr, "", "hub failed", 1},
		{"fallback uses hub", config.MCPHubFetchFallback, hubOK, localErr, "hub", "", 1},
		{"fallback local first", config.MCPHubFetchFallback, hubOK, localOK, "local", "", 0},
		{"fallback both fail", config.MCPHubFetchFallback, hubErr, localErr, "", "local failed", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.hub.calls = 0
			got, err := fetchPageWithFallback(context.Background(), "https://example.com", tc.mode, tc.hub, tc.local)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q err %v, want %q", got, err, tc.want)
			}
			if tc.hub.calls != tc.wantCalls {
				t.Fatalf("hub calls = %d, want %d", tc.hub.calls, tc.wantCalls)
			}
		})
	}
}

func TestApplyMCPHubFetchReusesSearchHub(t *testing.T) {
	conf := config.Default()
	conf.CleanFetch.Enabled = true
	conf.MCPHub.FetchMode = config.MCPHubFetchPrefer
	conf.MCPHub.Servers = []config.MCPHubServerConfig{{Name: "parallel"}}

	hub, err := mcphub.New(conf.MCPHub, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	oldSearch, oldInst, oldMode := searchapi, mcpHubFetchInst, mcpHubFetchMode
	t.Cleanup(func() {
		searchapi, mcpHubFetchInst, mcpHubFetchMode = oldSearch, oldInst, oldMode
	})
	searchapi = hub
	mcpHubFetchInst = nil
	applyMCPHubFetch(*conf)

	if mcpHubFetchInst != hub {
		t.Fatalf("fetch hub = %p, want search hub %p", mcpHubFetchInst, hub)
	}
}

func TestIsPDFURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://example.com/a.pdf":     true,
		"https://example.com/a.PDF?x=1": true,
		"https://example.com/article":   false,
		"https://example.com/pdf?x=1":   false,
		"not a url":                     false,
	} {
		if got := isPDFURL(raw); got != want {
			t.Fatalf("isPDFURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
