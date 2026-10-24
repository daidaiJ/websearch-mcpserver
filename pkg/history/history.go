// Package history 本地搜索历史（FTS5 全文检索）。
//
// smartsearch 每次成功搜索后把 query + 结果摘要落库，search_history MCP 工具
// 让模型「搜自己查过的东西」而不必重抓。数据只落本地 SQLite、不出网；
// 正文快照截断存储，隐私红线沿用遥测脱敏口径（不含密钥、不含错误原文）。
//
// 默认关闭（search_history.enabled 零值 = 不建库、不注册工具）；
// 独立保留期默认 30 天，过期行由后台清理协程删除。
package history

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"websearch/pkg/search/core"

	_ "modernc.org/sqlite"
)

const (
	// DefaultRetentionDays 历史默认保留天数。
	DefaultRetentionDays = 30
	// maxResultsPerRecord 单条历史最多保存的结果数（快照截断，控制库体积）。
	maxResultsPerRecord = 20
	// maxSnippetRunes 单结果正文快照最大字符数。
	maxSnippetRunes = 500
	// cleanupInterval 后台清理间隔。
	cleanupInterval = time.Hour
)

// Record 一条搜索历史及其结果快照。
type Record struct {
	ID          int64               `json:"id"`
	Query       string              `json:"query"`
	Mode        string              `json:"mode"` // web / academic
	ResultCount int                 `json:"result_count"`
	CreatedAt   time.Time           `json:"created_at"`
	Results     []core.SearchResult `json:"results,omitempty"`
}

// Store 本地搜索历史存储，并发安全。
type Store struct {
	db        *sql.DB
	retention time.Duration

	closeOnce sync.Once
	stopClean chan struct{}
}

// New 打开（并按需创建）历史库，启动保留期清理协程。
func New(storagePath string, retentionDays int) (*Store, error) {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建历史目录失败: %w", err)
	}
	// PRAGMA 与 cache/telemetry 同口径：WAL 并发读 + busy_timeout 写排队。
	dsn := storagePath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	db.SetMaxOpenConns(2)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS search_history (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			query        TEXT    NOT NULL,
			mode         TEXT    NOT NULL DEFAULT 'web',
			results      TEXT    NOT NULL DEFAULT '[]',
			result_count INTEGER NOT NULL DEFAULT 0,
			created_at   INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_search_history_created ON search_history(created_at);
		CREATE VIRTUAL TABLE IF NOT EXISTS search_history_fts USING fts5(
			query, titles, content='search_history', content_rowid='id'
		);
		CREATE TRIGGER IF NOT EXISTS search_history_ai AFTER INSERT ON search_history BEGIN
			INSERT INTO search_history_fts(rowid, query, titles)
			VALUES (new.id, new.query, new.results);
		END;
		CREATE TRIGGER IF NOT EXISTS search_history_ad AFTER DELETE ON search_history BEGIN
			INSERT INTO search_history_fts(search_history_fts, rowid, query, titles)
			VALUES ('delete', old.id, old.query, old.results);
		END;
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	s := &Store{db: db, retention: time.Duration(retentionDays) * 24 * time.Hour, stopClean: make(chan struct{})}
	// 启动即清一次过期行，之后周期清理。
	if err := s.Cleanup(); err != nil {
		db.Close()
		return nil, err
	}
	go s.cleanLoop()
	return s, nil
}

// Insert 记录一次搜索：query + 截断后的结果快照。失败只记日志不影响搜索主链路。
func (s *Store) Insert(query, mode string, results []core.SearchResult) error {
	if s == nil || s.db == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	snapshot := snapshotResults(results)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("history 序列化失败: %w", err)
	}
	// FTS titles 列复用 results JSON（触发器同步）；FTS 只需可分词文本。
	_, err = s.db.Exec(
		`INSERT INTO search_history (query, mode, results, result_count, created_at) VALUES (?, ?, ?, ?, ?)`,
		query, mode, string(raw), len(snapshot), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("history 写入失败: %w", err)
	}
	return nil
}

// Search 全文检索历史：FTS5 MATCH；query 为空时退化为按时间倒序的最近历史。
func (s *Store) Search(query string, limit, offset int) ([]Record, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	var rows *sql.Rows
	var err error
	if strings.TrimSpace(query) == "" {
		rows, err = s.db.Query(
			`SELECT id, query, mode, results, result_count, created_at
			 FROM search_history ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	} else {
		rows, err = s.db.Query(
			`SELECT h.id, h.query, h.mode, h.results, h.result_count, h.created_at
			 FROM search_history_fts f JOIN search_history h ON h.id = f.rowid
			 WHERE search_history_fts MATCH ?
			 ORDER BY h.created_at DESC, h.id DESC LIMIT ? OFFSET ?`, query, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("history 检索失败: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var raw string
		var created int64
		if err := rows.Scan(&r.ID, &r.Query, &r.Mode, &raw, &r.ResultCount, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(created, 0)
		if raw != "" && raw != "[]" {
			_ = json.Unmarshal([]byte(raw), &r.Results)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Cleanup 删除超过保留期的历史行（触发器同步 FTS 索引）。
func (s *Store) Cleanup() error {
	if s == nil || s.db == nil {
		return nil
	}
	cutoff := time.Now().Add(-s.retention).Unix()
	_, err := s.db.Exec(`DELETE FROM search_history WHERE created_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("history 清理失败: %w", err)
	}
	return nil
}

// Close 停止清理协程并关闭数据库。
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.stopClean)
		_ = s.db.Close()
	})
}

func (s *Store) cleanLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopClean:
			return
		case <-ticker.C:
			_ = s.Cleanup()
		}
	}
}

// snapshotResults 截断结果快照：最多 maxResultsPerRecord 条、正文每条 maxSnippetRunes。
func snapshotResults(results []core.SearchResult) []core.SearchResult {
	if len(results) > maxResultsPerRecord {
		results = results[:maxResultsPerRecord]
	}
	out := make([]core.SearchResult, 0, len(results))
	for _, r := range results {
		if runes := []rune(r.Content); len(runes) > maxSnippetRunes {
			r.Content = string(runes[:maxSnippetRunes]) + "…"
		}
		out = append(out, r)
	}
	return out
}
