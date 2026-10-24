package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search"
)

func TestToolSnapshotDoesNotProbeEverything(t *testing.T) {
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer endpoint.Close()
	restoreGlobals(t)
	oldCfg := everythingLazyCfg
	t.Cleanup(func() { everythingLazyCfg = oldCfg })
	everythingLazyCfg = &config.EverythingConfig{URL: endpoint.URL}
	publishTools(pendingTools())
	var readers sync.WaitGroup
	for range 16 {
		readers.Go(func() {
			for range 20 {
				snapshot := ToolSnapshot()
				if len(snapshot) != 6 || snapshot[4].State != "unprobed" {
					t.Error("snapshot must preserve the unprobed state")
				}
				snapshot[0].State = "modified copy"
			}
		})
	}
	readers.Wait()
	if calls.Load() != 0 || ToolSnapshot()[0].State != "pending" {
		t.Fatal("read-only snapshots probed Everything or shared mutable state")
	}
}

func TestSmartsearchRegistrationUsesActualGroupWithoutBing(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	oldCfg, oldInst := everythingLazyCfg, everythingInst
	t.Cleanup(func() { everythingLazyCfg, everythingInst = oldCfg, oldInst })
	everythingLazyCfg, everythingInst = nil, nil
	searchGroup = &search.SearchGroup{Primary: &mockSearch{name: "anysearch"}}
	conf := config.Config{Mode: config.ModeAnysearch}
	names := listToolNames(t, conf)
	if !slices.Contains(names, "smartsearch") {
		t.Fatalf("available Primary must register smartsearch without Bing: %v", names)
	}
	for _, state := range ToolSnapshot() {
		if state.Registered != slices.Contains(names, state.Name) {
			t.Fatalf("snapshot differs from ListTools: %+v, %v", state, names)
		}
	}
	if text := renderCapabilities(conf); !strings.Contains(text, "`smartsearch`") || strings.Contains(text, "`pdf_parser`") {
		t.Fatalf("capabilities must reflect registration, not config: %s", text)
	}
}
