package cache

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"websearch/pkg/search/core"
)

// TestNewEnablesWAL WAL 是并发读写的前提：journal_mode 持久在库文件头，
// 必须实际生效（历史实现只在注释里声明，从未执行 PRAGMA）。
func TestNewEnablesWAL(t *testing.T) {
	c := newTestCache(t)

	var mode string
	if err := c.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("读取 journal_mode 失败: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode 应为 wal，实际 %q", mode)
	}
}

// TestConcurrentReadersAndWriters 8 读 × 2 写并发压同一个库：
// 读命中/未命中都不得因写事务报 database is locked，写入不得丢失。
func TestConcurrentReadersAndWriters(t *testing.T) {
	c := newTestCache(t)
	if _, err := c.db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("设置 busy_timeout 失败: %v", err)
	}

	const (
		readers = 8
		writers = 2
		rounds  = 20
	)

	var wg sync.WaitGroup
	errCh := make(chan error, readers+writers)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				query := fmt.Sprintf("writer-%d-%d", w, i)
				results := []core.SearchResult{{Title: query, Url: "https://example.com/" + query}}
				if err := c.Store(query, "web", false, results, "s", 0); err != nil {
					errCh <- fmt.Errorf("Store(%s): %w", query, err)
					return
				}
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				query := fmt.Sprintf("writer-%d-%d", i%writers, i%rounds)
				rec, _, err := c.Lookup(query, "web", false)
				if err != nil {
					errCh <- fmt.Errorf("Lookup(%s): %w", query, err)
					return
				}
				if rec != nil && !strings.HasPrefix(rec.Query, "writer-") {
					errCh <- fmt.Errorf("Lookup(%s) 命中异常记录 %q", query, rec.Query)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}(r)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	var count int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM search_cache`).Scan(&count); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if want := writers * rounds; count != want {
		t.Fatalf("并发写入后应有 %d 行，实际 %d 行", want, count)
	}
}

// TestConcurrentCrossInstanceWriters 两个 Cache 实例指向同一库文件，
// 验证连接级 PRAGMA（busy_timeout/WAL）在独立连接池间同样生效。
func TestConcurrentCrossInstanceWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	c1, err := New(path)
	if err != nil {
		t.Fatalf("New(c1) 失败: %v", err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := New(path)
	if err != nil {
		t.Fatalf("New(c2) 失败: %v", err)
	}
	defer func() { _ = c2.Close() }()

	const rounds = 25
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for idx, c := range []*Cache{c1, c2} {
		wg.Add(1)
		go func(idx int, c *Cache) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				query := fmt.Sprintf("cross-%d-%d", idx, i)
				if err := c.Store(query, "web", false, nil, "", 0); err != nil {
					errCh <- fmt.Errorf("Store(%s): %w", query, err)
					return
				}
			}
		}(idx, c)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	var count int
	if err := c1.db.QueryRow(`SELECT COUNT(*) FROM search_cache`).Scan(&count); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if want := 2 * rounds; count != want {
		t.Fatalf("两个实例并发写入后应有 %d 行，实际 %d 行", want, count)
	}
}
