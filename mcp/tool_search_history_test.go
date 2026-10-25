package mcpserver

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/history"
	"websearch/pkg/search"
)

// TestCacheHitWritesSearchHistory 缓存命中也是一次完成的搜索：命中路径必须与
// 实时路径一样写历史（工具描述口径「每次 smartsearch 成功后自动写入」）。
func TestCacheHitWritesSearchHistory(t *testing.T) {
	srv := newMCPHTTPTestServer(t, config.Config{MCPStateless: true})
	c, err := cache.New(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cacheInst, cacheNamespace = c, "history-test"
	h, err := history.New(filepath.Join(t.TempDir(), "history.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	oldHistory := historyInst
	historyInst = h
	t.Cleanup(func() {
		historyInst = oldHistory
		h.Close()
	})

	searchapi = &mockSearch{name: "fixture", results: []search.SearchResult{{Title: "History article", Url: "https://example.com", Content: "body"}}}
	client := &http.Client{Timeout: 30 * time.Second}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smartsearch","arguments":{"query":"history fixture","fetch_top_n":0}}}`
	mcpHTTPPost(t, client, srv.URL+"/mcp", request)              // 第一次：实时搜索
	_, _, msg := mcpHTTPPost(t, client, srv.URL+"/mcp", request) // 第二次：缓存命中
	if !strings.Contains(jsonToolText(t, msg), "cache_age_seconds") {
		t.Fatal("第二次搜索应命中缓存")
	}
	records, err := h.Search("history fixture", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("实时搜索与缓存命中应各写一条历史，got %d", len(records))
	}
}

// TestFormatHistoryQueryTruncation 超长 query 在历史条目头部截断展示。
func TestFormatHistoryQueryTruncation(t *testing.T) {
	long := strings.Repeat("超", 300)
	text := formatHistory("", []history.Record{{Query: long, Mode: "web", ResultCount: 1}})
	if strings.Count(text, "超") != 200 {
		t.Fatalf("超长 query 应截断到 200 字符 + 省略号，got %d", strings.Count(text, "超"))
	}
	var recordLine string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			recordLine = line
			break
		}
	}
	if recordLine == "" || !strings.HasSuffix(strings.Split(recordLine, " ｜")[0], "…") {
		t.Fatalf("历史条目头的 query 段应以省略号结尾:\n%s", recordLine)
	}
}
