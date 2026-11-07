package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"websearch/pkg/cache"
	"websearch/pkg/search"
)

// ── B7：academicsearch 缓存键必须含 page ─────────────────────────────────────
// 旧实现键 = query|timeRange|engines（无 page），page>=2 在 TTL 内命中第一页
// 记录，分页静默退化为「永远返回第一页」。

type countingAcademic struct {
	calls  int
	pages  []int
	result search.AcademicSearchResult
}

func (c *countingAcademic) AcademicEngines() []string { return []string{"arxiv"} }
func (c *countingAcademic) SearchAcademicRaw(_ string, opts ...search.AcademicSearchOptions) (search.AcademicSearchResult, error) {
	c.calls++
	if len(opts) > 0 {
		c.pages = append(c.pages, opts[0].Page)
	}
	return c.result, nil
}

func setupAcademicCacheTest(t *testing.T) {
	t.Helper()
	initTestLogger()
	restoreGlobals(t)
	c, err := cache.New(t.TempDir() + "/cache.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	searchapi = fakeSearchAPI{}
	cacheInst, cacheNamespace = c, "academic-cache-key-test"
}

// TestAcademicCacheKeyIncludesPage 同一 query/timeRange/engines 下 page=1 与
// page=2 必须各自检索并各自缓存：第二次调用不得命中第一页记录。
func TestAcademicCacheKeyIncludesPage(t *testing.T) {
	setupAcademicCacheTest(t)
	page1 := &countingAcademic{result: search.AcademicSearchResult{
		Results: []search.SearchResult{{Title: "first page", Url: "https://example.com/1", Type: "paper"}},
	}}
	academicSearcher = page1
	if _, _, err := doAcademicSearch(context.Background(), "thermoacoustic", nil, "", 1); err != nil {
		t.Fatal(err)
	}
	// page=2：键不含 page 时这里会命中第一页缓存（calls 不增长）
	page2 := &countingAcademic{result: search.AcademicSearchResult{
		Results: []search.SearchResult{{Title: "second page", Url: "https://example.com/2", Type: "paper"}},
	}}
	academicSearcher = page2
	result, _, err := doAcademicSearch(context.Background(), "thermoacoustic", nil, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if page2.calls != 1 {
		t.Fatalf("page=2 必须真实检索（B7：不得命中第一页缓存），calls=%d", page2.calls)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if want := "second page"; !strings.Contains(text, want) {
		t.Fatalf("page=2 应返回第二页内容，实际不含 %q：%s", want, text)
	}
	// page=1 的缓存仍独立存在且未被 page=2 覆盖
	page1Again := &countingAcademic{}
	academicSearcher = page1Again
	if _, _, err := doAcademicSearch(context.Background(), "thermoacoustic", nil, "", 1); err != nil {
		t.Fatal(err)
	}
	if page1Again.calls != 0 {
		t.Fatalf("page=1 的缓存被 page=2 污染：calls=%d", page1Again.calls)
	}
}

// TestAcademicPageOneTwoNineKeysDiffer 固化键断言：page 1/2/9 构造出的键两两不同。
// 旧实现三页共用一个键（B7 的直接病灶），回退旧实现时本用例必须失败。
func TestAcademicPageOneTwoNineKeysDiffer(t *testing.T) {
	engines := []string{"arxiv", "crossref"}
	k1 := academicCacheKey("q", "year", engines, 1)
	k2 := academicCacheKey("q", "year", engines, 2)
	k9 := academicCacheKey("q", "year", engines, 9)
	if k1 == k2 || k2 == k9 || k1 == k9 {
		t.Fatalf("page 1/2/9 的缓存键必须互不相同: %q %q %q", k1, k2, k9)
	}
	if !strings.Contains(k1, "page=1") || !strings.Contains(k9, "page=9") {
		t.Fatalf("键应可读且可断言页码: %q %q", k1, k9)
	}
	// 同页码同参数必须稳定同键（缓存有效性的前提）
	if again := academicCacheKey("q", "year", engines, 2); again != k2 {
		t.Fatalf("同参数键应稳定: %q vs %q", again, k2)
	}
}

// TestAcademicCacheKeyClampsNonPositivePage MCP 省略 page（零值 0）与适配器
// clamp 后的 page=1 必须共用同一缓存键，否则缺省调用与显式 page=1 各写各的。
func TestAcademicCacheKeyClampsNonPositivePage(t *testing.T) {
	engines := []string{"arxiv"}
	k0 := academicCacheKey("q", "year", engines, 0)
	k1 := academicCacheKey("q", "year", engines, 1)
	kNeg := academicCacheKey("q", "year", engines, -3)
	if k0 != k1 || kNeg != k1 {
		t.Fatalf("page<=0 必须与 page=1 同键: 0=%q 1=%q -3=%q", k0, k1, kNeg)
	}
	if !strings.Contains(k1, "page=1") {
		t.Fatalf("钳制后键应可读为 page=1: %q", k1)
	}
}
