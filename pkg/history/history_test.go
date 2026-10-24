package history

import (
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/search/core"
)

func newTestStore(t *testing.T, retentionDays int) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "h", "search-history.db"), retentionDays)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func sampleResults() []core.SearchResult {
	return []core.SearchResult{
		{Title: "Go 泛型教程", Url: "https://go.dev/doc/generics", Content: "泛型入门", PublishDate: "2026-10-01", DateSource: core.DateSourceStructured},
		{Title: "Go 博客", Url: "https://go.dev/blog", Content: "官方博客"},
	}
}

func TestInsertAndSearch(t *testing.T) {
	s := newTestStore(t, 0)
	if err := s.Insert("Go 泛型 教程", "web", sampleResults()); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert("crispr 基因编辑", "web", nil); err != nil {
		t.Fatal(err)
	}

	// FTS 命中 query
	recs, err := s.Search("泛型", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Query != "Go 泛型 教程" || recs[0].Mode != "web" {
		t.Fatalf("recs = %+v", recs)
	}
	if len(recs[0].Results) != 2 || recs[0].Results[0].Url != "https://go.dev/doc/generics" {
		t.Fatalf("results snapshot = %+v", recs[0].Results)
	}

	// FTS 命中结果标题/正文（titles 列来自 results JSON）
	recs, err = s.Search("crispr", 10, 0)
	if err != nil || len(recs) != 1 {
		t.Fatalf("crispr recs = %+v err = %v", recs, err)
	}
	if recs[0].ResultCount != 0 || recs[0].Results != nil {
		t.Fatalf("empty-result record should keep zero results: %+v", recs[0])
	}

	// 空 query = 最近历史倒序
	recs, err = s.Search("", 10, 0)
	if err != nil || len(recs) != 2 || recs[0].Query != "crispr 基因编辑" {
		t.Fatalf("recent recs = %+v err = %v", recs, err)
	}

	// 无命中是合法结果（空列表，非错误）
	recs, err = s.Search("不存在的词xyz", 10, 0)
	if err != nil || len(recs) != 0 {
		t.Fatalf("no-hit recs = %+v err = %v", recs, err)
	}
}

// TestSearchSpecialCharsFTSSafe 特殊字符查询不得触发 FTS5 语法错误：
// 引号/括号/星号/连字符/AND-OR 操作符经 ftsQuery 引号包裹后退化为词组匹配。
func TestSearchSpecialCharsFTSSafe(t *testing.T) {
	s := newTestStore(t, 0)
	if err := s.Insert(`golang "context" 包`, "web", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert("rust OR go 教程", "web", nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`golang "context"`, // 引号
		"golang (context)", // 括号
		"go*",              // 前缀星号
		"golang OR rust",   // FTS5 操作符
		"c++ -lang",        // 连字符
		"NEAR",             // 纯操作符词
	} {
		recs, err := s.Search(q, 10, 0)
		if err != nil {
			t.Fatalf("query %q 不应报错: %v", q, err)
		}
		_ = recs // 命中数不校验（引号包裹后操作符退化为普通词），只要不报错
	}
	// 简单词组语义不变：多词隐式 AND 仍命中
	recs, err := s.Search("golang 包", 10, 0)
	if err != nil || len(recs) != 1 || recs[0].Query != `golang "context" 包` {
		t.Fatalf("简单词组 AND 语义应保留: recs=%+v err=%v", recs, err)
	}
}

func TestInsertEmptyQueryIgnored(t *testing.T) {
	s := newTestStore(t, 0)
	if err := s.Insert("   ", "web", sampleResults()); err != nil {
		t.Fatal(err)
	}
	recs, err := s.Search("", 10, 0)
	if err != nil || len(recs) != 0 {
		t.Fatalf("blank query must not be recorded: %+v err=%v", recs, err)
	}
}

func TestSnapshotTruncation(t *testing.T) {
	s := newTestStore(t, 0)
	long := make([]core.SearchResult, 30)
	for i := range long {
		long[i] = core.SearchResult{Title: "t", Url: "https://example.com", Content: string(make([]rune, 800))}
	}
	if err := s.Insert("长结果", "web", long); err != nil {
		t.Fatal(err)
	}
	recs, err := s.Search("长结果", 10, 0)
	if err != nil || len(recs) != 1 {
		t.Fatalf("recs = %+v err = %v", recs, err)
	}
	if len(recs[0].Results) != maxResultsPerRecord {
		t.Fatalf("results = %d, want %d", len(recs[0].Results), maxResultsPerRecord)
	}
	if runes := []rune(recs[0].Results[0].Content); len(runes) > maxSnippetRunes+1 {
		t.Fatalf("snippet runes = %d, want <= %d", len(runes), maxSnippetRunes+1)
	}
}

func TestRetentionCleanup(t *testing.T) {
	s := newTestStore(t, 0)
	if err := s.Insert("旧查询", "web", sampleResults()); err != nil {
		t.Fatal(err)
	}
	// 手工把 created_at 拨到 31 天前
	old := time.Now().Add(-31 * 24 * time.Hour).Unix()
	if _, err := s.db.Exec(`UPDATE search_history SET created_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	recs, err := s.Search("", 10, 0)
	if err != nil || len(recs) != 0 {
		t.Fatalf("expired rows must be removed: %+v err = %v", recs, err)
	}
}

func TestNilStoreSafe(t *testing.T) {
	var s *Store
	if err := s.Insert("q", "web", nil); err != nil {
		t.Fatalf("nil store insert must be a no-op, got %v", err)
	}
	recs, err := s.Search("", 10, 0)
	if err != nil || recs != nil {
		t.Fatalf("nil store search must be a no-op, got %+v err=%v", recs, err)
	}
	s.Close() // nil 接收者不得 panic
}
