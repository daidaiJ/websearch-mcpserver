package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"websearch/pkg/config"
)

func TestManualEverythingProbeRegistersToolForConnectedClient(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	oldCfg, oldInst := everythingLazyCfg, everythingInst
	oldRoots, oldMax, oldNoise, oldAlign := everythingRoots, everythingMaxResults, everythingNoise, everythingMinAlign
	oldWaiting := waitingForEverything
	oldConnectivity, oldState := EverythingConnectivity(), ToolSnapshot()
	t.Cleanup(func() {
		everythingLazyCfg, everythingInst = oldCfg, oldInst
		everythingRoots, everythingMaxResults, everythingNoise, everythingMinAlign = oldRoots, oldMax, oldNoise, oldAlign
		waitingForEverything = oldWaiting
		setEverythingConnectivity(oldConnectivity)
		publishTools(oldState)
	})

	var available atomic.Bool
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !available.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"totalResults":0,"results":[]}`))
	}))
	defer endpoint.Close()
	conf := config.Config{Everything: config.EverythingConfig{URL: endpoint.URL}}
	everythingLazyCfg, everythingInst = &conf.Everything, nil
	waitingForEverything = nil

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := NewMCPServer(conf, nil).Connect(ctx, t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	hasFileSearch := func() bool {
		result, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(result.Tools, func(tool *mcp.Tool) bool { return tool.Name == "file_search" })
	}
	if hasFileSearch() || calls.Load() != 1 {
		t.Fatalf("unavailable tool registered or tried more than once: %d", calls.Load())
	}
	available.Store(true)
	if err := ReprobeEverything(ctx, conf.Everything); err != nil {
		t.Fatal(err)
	}
	if !hasFileSearch() || EverythingConnectivity() != "available" || calls.Load() != 2 {
		t.Fatalf("existing session did not see recovered tool: status=%s calls=%d", EverythingConnectivity(), calls.Load())
	}
}
