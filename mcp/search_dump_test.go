package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"websearch/pkg/config"
	"websearch/pkg/search"
	searchcore "websearch/pkg/search/core"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setDumpTestEnv 设置落盘测试用的全局状态并在测试结束后恢复。
func setDumpTestEnv(t *testing.T, dir string, conf config.SmartSearchConfig) {
	t.Helper()
	oldDir := searchDumpDir
	oldConf := smartSearchConf
	searchDumpDir = dir
	smartSearchConf = conf
	t.Cleanup(func() {
		searchDumpDir = oldDir
		smartSearchConf = oldConf
	})
}

// TestDumpSearchResultsIfOversize_UnderLimit 未超限时不落盘。
func TestDumpSearchResultsIfOversize_UnderLimit(t *testing.T) {
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1000})
	notice, dumped := dumpSearchResultsIfOversize(3, strings.Repeat("结果内容", 10))
	if dumped {
		t.Fatalf("未超限不应落盘: %q", notice)
	}
}

// TestDumpSearchResultsIfOversize_Disabled 负数配置禁用落盘（旧版恒内联行为）。
func TestDumpSearchResultsIfOversize_Disabled(t *testing.T) {
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: -1})
	notice, dumped := dumpSearchResultsIfOversize(3, strings.Repeat("结果内容", 1000))
	if dumped {
		t.Fatalf("禁用配置下不应落盘: %q", notice)
	}
}

// TestDumpSearchResultsIfOversize_OverLimit 超限时整体落盘：响应只留统计/路径/读取提示，
// 完整内容写入文件且文件名匹配本程序命名模式。
func TestDumpSearchResultsIfOversize_OverLimit(t *testing.T) {
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 100})
	body := strings.Repeat("搜索结果正文", 40) // 240 字符 > 100
	notice, dumped := dumpSearchResultsIfOversize(7, body)
	if !dumped {
		t.Fatal("超限应落盘")
	}
	for _, want := range []string{"7 条", "240 字符", "100 字符", "**文件路径**", "读取提示"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("通知缺少 %q: %q", want, notice)
		}
	}
	if strings.Contains(notice, body) {
		t.Fatal("响应不应内联完整结果")
	}
	// 提取路径并验证文件内容完整
	start := strings.Index(notice, "`") + 1
	end := strings.LastIndex(notice, "`")
	path := notice[start:end]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("落盘文件读取失败: %v", err)
	}
	if string(data) != body {
		t.Fatal("落盘内容必须与渲染结果逐字节一致（零丢失）")
	}
	if !searchDumpFileRe.MatchString(filepath.Base(path)) {
		t.Fatalf("文件名应匹配 search-* 模式: %s", filepath.Base(path))
	}
}

// TestDumpSearchResultsIfOversize_DefaultLimit 零值配置 = 默认 32768。
func TestDumpSearchResultsIfOversize_DefaultLimit(t *testing.T) {
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{})
	limit, enabled := smartSearchConf.InlineMaxCharsOrDefault()
	if !enabled || limit != 32768 {
		t.Fatalf("零值应取默认 32768，got limit=%d enabled=%v", limit, enabled)
	}
}

// TestFinishWebSearch_OversizeDumps 集成：超限时实时 raw 路径响应只留
// 溯源头 + 落盘通知 + 失败清单（永不裁剪），完整正文只进文件。
func TestFinishWebSearch_OversizeDumps(t *testing.T) {
	initTestLogger()
	restoreGlobals(t)
	dir := t.TempDir()
	searchDumpDir = dir
	t.Cleanup(func() { searchDumpDir = "" })
	smartSearchConf = config.SmartSearchConfig{InlineMaxChars: 100}
	cacheInst = nil

	body := strings.Repeat("搜索结果正文内容", 30) // 210 字符 > 100
	searchapi = &mockSearch{merged: body}
	diag := &searchcore.SearchDiagnostics{}
	diag.AddFailures("bing", searchcore.FailureRateLimit, "HTTP 429 rate-limited (cooling down 30s)")

	resp, _, err := finishWebSearch(context.Background(), nil, "q", "", "q",
		[]search.SearchResult{{Title: "t", Url: "https://e.com/x"}}, diag, time.Hour,
		provenance{retrievedAt: time.Now()})
	if err != nil {
		t.Fatalf("finishWebSearch failed: %v", err)
	}
	tc, ok := resp.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", resp.Content[0])
	}
	for _, want := range []string{"retrieved_at", "已整体保存到文件", "**文件路径**", "rate_limit", "bing"} {
		if !strings.Contains(tc.Text, want) {
			t.Fatalf("响应缺少 %q: %s", want, tc.Text)
		}
	}
	if !strings.Contains(tc.Text, "部分引擎本次失败") || !strings.Contains(tc.Text, "bing") {
		t.Fatalf("失败清单应随落盘响应保留（永不裁剪）: %s", tc.Text)
	}
	if strings.Contains(tc.Text, body) {
		t.Fatal("响应不应内联完整结果")
	}
}

// TestCleanupSearchDumps 惰性清理只删超 TTL 且匹配 search-* 模式的文件，
// 目录内其它文件（含 webfetch 落盘命名）一律不动。
func TestCleanupSearchDumps(t *testing.T) {
	dir := t.TempDir()
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	fresh := filepath.Join(dir, "search-20260101_120000-aaaaaaaa.md")
	stale := filepath.Join(dir, "search-20251201_120000-bbbbbbbb.md")
	foreign := filepath.Join(dir, "20251201_120000_title_abcdef.md") // webfetch 命名
	user := filepath.Join(dir, "notes.md")
	for _, p := range []string{fresh, stale, foreign, user} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// fresh 保持当前 mtime；stale/foreign 回拨到 8 天前
	staleT := oldTime
	for _, p := range []string{stale, foreign} {
		if err := os.Chtimes(p, staleT, staleT); err != nil {
			t.Fatal(err)
		}
	}

	cleanupSearchDumps(dir)

	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("未过期的 search 文件不应被清理")
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("超 TTL 的 search 文件应被清理")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("非 search-* 命名的过期文件（webfetch 落盘）不应被清理")
	}
	if _, err := os.Stat(user); err != nil {
		t.Fatal("用户文件不应被清理")
	}
}
