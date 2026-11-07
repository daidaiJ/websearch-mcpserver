package cache

import (
	"errors"
	"path/filepath"
	"testing"
)

func requestView(t *testing.T, c *Cache, namespace string) *Cache {
	t.Helper()
	view, err := c.ForRequest(namespace)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestClearRejectsInFlightStoreAndSummaryAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := requestView(t, c, "source-a")
	if err := old.Store("q", "intent", false, nil, "", 0); err != nil {
		t.Fatal(err)
	}
	if count, err := c.Clear(); err != nil || count != 1 {
		t.Fatalf("Clear = %d, %v", count, err)
	}
	// A second connection models a restarted process while the old request ends.
	restarted, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	current := requestView(t, restarted, "source-a")
	if err := old.Store("q", "intent", false, nil, "stale result", 0); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("作废写入应报 ErrStaleGeneration: %v", err)
	}
	var count int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM search_cache`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("in-flight request reinserted deleted data: count=%d err=%v", count, err)
	}
	if err := current.Store("q", "intent", false, nil, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := old.UpdateSummary("q", "intent", "stale summary"); err != nil {
		t.Fatal(err)
	}
	rec, _, err := current.Lookup("q", "intent", false)
	if err != nil || rec == nil || rec.Summary != "" || rec.Query != "q" {
		t.Fatalf("late summary changed the new generation: %+v, %v", rec, err)
	}
	if rec, _, err := old.Lookup("q", "intent", false); err != nil || rec != nil {
		t.Fatalf("old generation read new data: %+v, %v", rec, err)
	}
}

// TestStoreStaleGenerationReported 代次守卫作废的写入必须报 ErrStaleGeneration，
// 正常写入仍返回 nil（B10：调用方要能区分「写成功」与「被代次丢弃」）。
// 注意 Lookup 没有对称改动：它以含代次的键查询，正确性靠 Clear 的 DELETE 兜底
// （docs/state-machines.md §3.1），与写入侧的显式守卫不同构。
func TestStoreStaleGenerationReported(t *testing.T) {
	c := newTestCache(t)
	old := requestView(t, c, "source-a")
	if err := old.Store("q", "intent", false, nil, "", 0); err != nil {
		t.Fatalf("正常代次写入应返回 nil: %v", err)
	}
	if _, err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := old.Store("q", "intent", false, nil, "stale", 0); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("Clear 后旧视图的 Store 应返回 ErrStaleGeneration，实际 %v", err)
	}
	current := requestView(t, c, "source-a")
	if err := current.Store("q", "intent", false, nil, "current", 0); err != nil {
		t.Fatalf("新代次写入应返回 nil: %v", err)
	}
	if rec, _, err := current.Lookup("q", "intent", false); err != nil || rec == nil || rec.Summary != "current" {
		t.Fatalf("被作废的写入不得污染新代次: %+v, %v", rec, err)
	}
}

func TestRequestNamespacesIsolateLegacyAndOtherSources(t *testing.T) {
	c := newTestCache(t)
	if err := c.Store("q", "", false, nil, "legacy", 0); err != nil {
		t.Fatal(err)
	}
	a := requestView(t, c, "source-a")
	if rec, _, err := a.Lookup("q", "", false); err != nil || rec != nil {
		t.Fatalf("unscoped legacy cache was trusted: %+v, %v", rec, err)
	}
	if err := a.Store("q", "", false, nil, "source-a-result", 0); err != nil {
		t.Fatal(err)
	}
	b := requestView(t, c, "source-b")
	if rec, _, err := b.Lookup("q", "", false); err != nil || rec != nil {
		t.Fatalf("different source configuration reused data: %+v, %v", rec, err)
	}
	if rec, _, err := requestView(t, c, "source-a").Lookup("q", "", false); err != nil || rec == nil || rec.Summary != "source-a-result" {
		t.Fatalf("same configuration should reuse current-generation cache: %+v, %v", rec, err)
	}
}
