package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
	"websearch/pkg/search/core"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestStoreUpsertSameQuery 同一 query+intent+academic 存储两次，表中应只有 1 行且内容被刷新。
func TestStoreUpsertSameQuery(t *testing.T) {
	c := newTestCache(t)

	results1 := []core.SearchResult{{Title: "first", Url: "https://example.com/1"}}
	if err := c.Store("golang", "web", false, results1, "summary-1", 0); err != nil {
		t.Fatalf("第一次 Store 失败: %v", err)
	}
	results2 := []core.SearchResult{{Title: "second", Url: "https://example.com/2"}}
	if err := c.Store("golang", "web", false, results2, "summary-2", 0); err != nil {
		t.Fatalf("第二次 Store 失败: %v", err)
	}

	var count int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM search_cache`).Scan(&count); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("同 query 存储两次后应只有 1 行，实际 %d 行", count)
	}

	rec, hitType, err := c.Lookup("golang", "web", false)
	if err != nil {
		t.Fatalf("Lookup 失败: %v", err)
	}
	if hitType != "exact_intent" {
		t.Fatalf("命中类型应为 exact_intent，实际 %s", hitType)
	}
	if rec.Summary != "summary-2" {
		t.Fatalf("summary 应被刷新为 summary-2，实际 %q", rec.Summary)
	}
	got, err := rec.GetRawResults()
	if err != nil {
		t.Fatalf("GetRawResults 失败: %v", err)
	}
	if len(got) != 1 || got[0].Title != "second" {
		t.Fatalf("raw_results 应被刷新为 second，实际 %+v", got)
	}
}

// TestStoreDifferentIntentSeparateRows 不同 intent/academic 应各自成行。
func TestStoreDifferentIntentSeparateRows(t *testing.T) {
	c := newTestCache(t)

	if err := c.Store("golang", "web", false, nil, "", 0); err != nil {
		t.Fatalf("Store web 失败: %v", err)
	}
	if err := c.Store("golang", "academic", true, nil, "", 0); err != nil {
		t.Fatalf("Store academic 失败: %v", err)
	}

	var count int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM search_cache`).Scan(&count); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("不同 intent 应各自成行，实际 %d 行", count)
	}
}

// TestLookupExpiresByTTL 超过 TTL 的记录应视为未命中并惰性删除；
// 未过期记录正常命中；ttl<=0 落 DefaultTTL。
func TestLookupExpiresByTTL(t *testing.T) {
	c := newTestCache(t)

	// 短 TTL 记录：写入后人工把 created_at 回拨，模拟跨过 TTL 的陈旧记录
	if err := c.Store("stale", "web", false, nil, "", time.Second); err != nil {
		t.Fatalf("Store stale 失败: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE search_cache SET created_at = created_at - 10 WHERE query = 'stale'`); err != nil {
		t.Fatalf("回拨 created_at 失败: %v", err)
	}
	if _, _, err := c.Lookup("stale", "web", false); err != nil {
		t.Fatalf("Lookup 不应报错: %v", err)
	}
	var count int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM search_cache WHERE query = 'stale'`).Scan(&count); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("过期记录应被惰性删除，实际仍在库中 %d 行", count)
	}

	// 默认 TTL（ttl=0 → DefaultTTL）：刚写入的记录应正常命中
	if err := c.Store("fresh", "web", false, nil, "", 0); err != nil {
		t.Fatalf("Store fresh 失败: %v", err)
	}
	rec, hitType, err := c.Lookup("fresh", "web", false)
	if err != nil || rec == nil || hitType == "miss" {
		t.Fatalf("未过期记录应命中: rec=%v hitType=%s err=%v", rec, hitType, err)
	}
	if rec.TTLSeconds != int64(DefaultTTL/time.Second) {
		t.Fatalf("ttl=0 应落 DefaultTTL=%d 秒，实际 %d", int64(DefaultTTL/time.Second), rec.TTLSeconds)
	}
}

// TestTTLForFreshness freshness 分桶映射：day→1h、month→24h、其它→默认 6h。
func TestTTLForFreshness(t *testing.T) {
	cases := map[string]time.Duration{
		"day":   time.Hour,
		"week":  DefaultTTL,
		"month": 24 * time.Hour,
		"":      DefaultTTL,
		"year":  DefaultTTL,
	}
	for in, want := range cases {
		if got := TTLForFreshness(in); got != want {
			t.Fatalf("TTLForFreshness(%q) = %v，期望 %v", in, got, want)
		}
	}
}

// TestNewDedupesLegacyDuplicates 旧库含重复行时，New 应去重（保留每组 last_hit_at 最大者）并成功建唯一索引。
func TestNewDedupesLegacyDuplicates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	// 模拟旧库：无唯一索引，含 (query, intent, academic) 重复行
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE search_cache (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			query       TEXT    NOT NULL,
			intent      TEXT    NOT NULL DEFAULT '',
			academic    INTEGER NOT NULL DEFAULT 0,
			raw_results TEXT    NOT NULL DEFAULT '[]',
			summary     TEXT    NOT NULL DEFAULT '',
			created_at  INTEGER NOT NULL,
			last_hit_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO search_cache (query, intent, academic, raw_results, summary, created_at, last_hit_at) VALUES
			('golang', 'web', 0, '[]', 'old', 100, 100),
			('golang', 'web', 0, '[]', 'mid', 200, 200),
			('golang', 'web', 0, '[]', 'new', 300, 300),
			('golang', 'academic', 1, '[]', '', 400, 400)
	`)
	if err != nil {
		t.Fatalf("插入旧数据失败: %v", err)
	}
	db.Close()

	c, err := New(dbPath)
	if err != nil {
		t.Fatalf("New 应成功去重并建唯一索引: %v", err)
	}
	defer c.Close()

	var count int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM search_cache`).Scan(&count); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("去重后应剩 2 行，实际 %d 行", count)
	}

	// 保留的是 last_hit_at 最大（300）的那条
	var summary string
	var lastHit int64
	if err := c.db.QueryRow(
		`SELECT summary, last_hit_at FROM search_cache WHERE query = 'golang' AND intent = 'web'`,
	).Scan(&summary, &lastHit); err != nil {
		t.Fatalf("查询保留行失败: %v", err)
	}
	if summary != "new" || lastHit != 300 {
		t.Fatalf("应保留 last_hit_at=300 的行，实际 summary=%q last_hit_at=%d", summary, lastHit)
	}

	// 唯一索引已生效：再插同组行应失败
	_, err = c.db.Exec(`
		INSERT INTO search_cache (query, intent, academic, raw_results, summary, created_at, last_hit_at)
		VALUES ('golang', 'web', 0, '[]', '', 999, 999)
	`)
	if err == nil {
		t.Fatal("唯一索引应拒绝重复插入")
	}
}
