package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"websearch/pkg/log"
	"websearch/pkg/search"

	_ "modernc.org/sqlite"
)

// CacheRecord 缓存记录
type CacheRecord struct {
	ID          int64     `json:"id"`
	Query       string    `json:"query"`
	Intent      string    `json:"intent"`
	RawResults  string    `json:"raw_results"`  // []SearchResult JSON
	Summary     string    `json:"summary"`       // LLM 摘要文本，可能为空
	CreatedAt   time.Time `json:"created_at"`    // 存储时间
	LastHitAt   time.Time `json:"last_hit_at"`   // 最近一次命中时间
}

// Cache SQLite 缓存层，并发安全
type Cache struct {
	db *sql.DB
}

// New 创建缓存实例，自动建表
func New(storagePath string) (*Cache, error) {
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}

	db, err := sql.Open("sqlite", storagePath)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	// SQLite WAL 模式 + 连接池配置，保证并发安全
	db.SetMaxOpenConns(1) // SQLite 单写者模式，串行化写操作

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS search_cache (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			query       TEXT    NOT NULL,
			intent      TEXT    NOT NULL DEFAULT '',
			raw_results TEXT    NOT NULL DEFAULT '[]',
			summary     TEXT    NOT NULL DEFAULT '',
			created_at  INTEGER NOT NULL,
			last_hit_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_query ON search_cache(query);
		CREATE INDEX IF NOT EXISTS idx_query_intent ON search_cache(query, intent);
		CREATE INDEX IF NOT EXISTS idx_last_hit ON search_cache(last_hit_at);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}

	log.Infof("缓存层已初始化: %s", storagePath)
	return &Cache{db: db}, nil
}

// Close 关闭数据库连接
func (c *Cache) Close() error {
	return c.db.Close()
}

// Lookup 查询缓存，单次查询同时覆盖精确匹配和仅 query 匹配
// 返回值: record(可能为nil), hitType("exact_intent" / "query_only" / "miss")
func (c *Cache) Lookup(query, intent string) (*CacheRecord, string, error) {
	now := time.Now().Unix()

	var rec CacheRecord
	var createdAt, lastHitAt int64
	var hitType string

	// 单次查询：WHERE query = ?，通过 CASE 判断 intent 是否同时命中
	// ORDER BY 优先返回 exact_intent，再按 last_hit_at 降序
	err := c.db.QueryRow(`
		SELECT id, query, intent, raw_results, summary, created_at, last_hit_at,
		       CASE WHEN intent = ? THEN 'exact_intent' ELSE 'query_only' END
		  FROM search_cache
		 WHERE query = ?
		 ORDER BY
		       CASE WHEN intent = ? THEN 0 ELSE 1 END,
		       last_hit_at DESC
		 LIMIT 1`,
		intent, query, intent,
	).Scan(&rec.ID, &rec.Query, &rec.Intent, &rec.RawResults, &rec.Summary, &createdAt, &lastHitAt, &hitType)

	if err == sql.ErrNoRows {
		return nil, "miss", nil
	}
	if err != nil {
		return nil, "miss", fmt.Errorf("缓存查询失败: %w", err)
	}

	rec.CreatedAt = time.Unix(createdAt, 0)
	rec.LastHitAt = time.Unix(lastHitAt, 0)
	c.touchLastHit(rec.ID, now)
	return &rec, hitType, nil
}

// touchLastHit 更新命中时间
func (c *Cache) touchLastHit(id int64, now int64) {
	_, _ = c.db.Exec(`UPDATE search_cache SET last_hit_at = ? WHERE id = ?`, now, id)
}

// Store 存储缓存记录
func (c *Cache) Store(query, intent string, results []search.SearchResult, summary string) error {
	now := time.Now().Unix()
	rawJSON, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("序列化搜索结果失败: %w", err)
	}

	_, err = c.db.Exec(
		`INSERT INTO search_cache (query, intent, raw_results, summary, created_at, last_hit_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		query, intent, string(rawJSON), summary, now, now,
	)
	if err != nil {
		return fmt.Errorf("缓存写入失败: %w", err)
	}

	log.Infof("缓存已存储: query=%q, intent=%q, has_summary=%v", query, intent, summary != "")
	return nil
}

// UpdateSummary 更新已有记录的摘要（query+intent 精确匹配）
func (c *Cache) UpdateSummary(query, intent string, summary string) error {
	_, err := c.db.Exec(
		`UPDATE search_cache SET summary = ? WHERE query = ? AND intent = ? AND summary = ''`,
		summary, query, intent,
	)
	return err
}

// GetRawResults 从 CacheRecord 反序列化搜索结果
func (r *CacheRecord) GetRawResults() ([]search.SearchResult, error) {
	var results []search.SearchResult
	if err := json.Unmarshal([]byte(r.RawResults), &results); err != nil {
		return nil, fmt.Errorf("反序列化缓存结果失败: %w", err)
	}
	return results, nil
}

// EvictStale 清理超过指定时间未被再次命中的记录
func (c *Cache) EvictStale(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	result, err := c.db.Exec(`DELETE FROM search_cache WHERE last_hit_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("清理缓存失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		log.Infof("缓存清理: 删除 %d 条超过 %v 未命中的记录", affected, maxAge)
	}
	return affected, nil
}
