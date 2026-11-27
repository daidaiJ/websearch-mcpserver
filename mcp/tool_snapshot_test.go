package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestSearchHistoryRegistrationDistinguishesDisabledAndUnavailable(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	oldHistory, oldCfg, oldInst := historyInst, everythingLazyCfg, everythingInst
	t.Cleanup(func() { historyInst, everythingLazyCfg, everythingInst = oldHistory, oldCfg, oldInst })
	historyInst, everythingLazyCfg, everythingInst = nil, nil, nil

	for _, tc := range []struct {
		enabled bool
		want    string
	}{{false, "disabled"}, {true, "unavailable"}} {
		root := t.TempDir()
		conf := config.Config{SearchHistory: config.SearchHistoryConfig{
			Enabled: &tc.enabled, StoragePath: filepath.Join(root, "history.db", "invalid.db"),
		}}
		// A regular file used as a parent directory cannot open as a history store.
		if err := os.WriteFile(filepath.Join(root, "history.db"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := WithSearchHistory(conf)(); err != nil {
			t.Fatal(err)
		}
		if names := listToolNames(t, conf); slices.Contains(names, "search_history") {
			t.Fatal("history without a store was listed as callable")
		}
		snapshot := ToolSnapshot()
		index := slices.IndexFunc(snapshot, func(state ToolState) bool { return state.Name == "search_history" })
		if index < 0 || snapshot[index].State != tc.want || snapshot[index].Registered {
			t.Fatalf("history enabled=%v should report %s: %+v", tc.enabled, tc.want, snapshot)
		}
	}
}
