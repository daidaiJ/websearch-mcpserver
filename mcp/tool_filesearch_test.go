package mcpserver

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"websearch/pkg/fetch/everything"
)

func TestFormatFileSearchResultEmpty(t *testing.T) {
	out := formatFileSearchResult(`*.go`, fileSearchPage{Total: 0}, nil, fileSearchTimeFormats["datetime"])
	if !strings.Contains(out, "没有匹配") {
		t.Errorf("空结果提示缺失: %q", out)
	}
}

func TestFormatFileSearchResultTimeFormats(t *testing.T) {
	item := everything.Item{Name: "factory.go", Path: `d:\code\ai\websearch\pkg\search\`, Size: "17408", DateModified: "134353169800707240"}
	items := []everything.Item{item}

	// datetime：本地时区年月日时分秒（与实机转换一致）
	datetime := fileSearchTimeFormats["datetime"](item)
	if datetime != "2026-10-01 16:29:40" {
		t.Errorf("datetime 格式错误: %q", datetime)
	}

	// iso：ISO 8601 UTC
	iso := fileSearchTimeFormats["iso"](item)
	if ts, err := time.Parse(time.RFC3339, iso); err != nil || !strings.HasSuffix(iso, "Z") {
		t.Errorf("iso 格式错误: %q (err=%v)", iso, err)
	} else if ts.Format("2006-01-02 15:04:05") != "2026-10-01 08:29:40" {
		t.Errorf("iso UTC 时间错误: %q", iso)
	}

	// filetime：原始值透传
	if ft := fileSearchTimeFormats["filetime"](item); ft != "134353169800707240" {
		t.Errorf("filetime 应原样透传: %q", ft)
	}

	out := formatFileSearchResult(`factory`, fileSearchPage{Page: 1, PageSize: 10, Total: 1, Candidate: 30}, items, fileSearchTimeFormats["datetime"])
	for _, want := range []string{"factory.go", "d:\\code\\ai\\websearch\\pkg\\search\\", "17.0 KB", "2026-10-01 16:29:40"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q: %q", want, out)
		}
	}
	for _, want := range []string{"命中 1 条", "第 1 页返回 1 条", "每页最多 10 条"} {
		if !strings.Contains(out, want) {
			t.Errorf("分页头缺少 %q: %q", want, out)
		}
	}
	if strings.Contains(out, "page=2") {
		t.Errorf("无更多结果时不应提示翻页: %q", out)
	}
}

// TestFormatFileSearchResultHints 覆盖尾部三类去向：还有下一页 / 候选池触顶无法再翻 / 弱匹配被过滤。
func TestFormatFileSearchResultHints(t *testing.T) {
	items := []everything.Item{{Name: "a.go", Path: `d:\`}}

	more := formatFileSearchResult(`a`, fileSearchPage{Page: 2, PageSize: 10, Total: 5000, Candidate: 60, More: true}, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(more, "page=3") {
		t.Errorf("翻页提示缺失: %q", more)
	}

	// 深度触顶：候选池 600（page 60 × 每页 10 × 3 超采）后取不到新候选
	capped := formatFileSearchResult(`a`, fileSearchPage{Page: 60, PageSize: 10, Total: 5000, Candidate: 600, Capped: true}, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(capped, "候选池已达上限") || strings.Contains(capped, "page=61") {
		t.Errorf("触顶提示错误: %q", capped)
	}

	// 命中数在候选池内但被阈值过滤掉：如实说明过滤后保留数
	filtered := formatFileSearchResult(`a`, fileSearchPage{Page: 1, PageSize: 10, Total: 25, Candidate: 30}, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(filtered, "过滤后保留 1 条") {
		t.Errorf("过滤提示缺失: %q", filtered)
	}

	// 空页：页码越界
	empty := formatFileSearchResult(`a`, fileSearchPage{Page: 5, PageSize: 10, Total: 25, Candidate: 150}, nil, fileSearchTimeFormats["datetime"])
	if !strings.Contains(empty, "第 5 页没有结果") {
		t.Errorf("空页提示缺失: %q", empty)
	}

	// 空页：候选池触顶
	cappedEmpty := formatFileSearchResult(`a`, fileSearchPage{Page: 60, PageSize: 10, Total: 5000, Candidate: 600, Capped: true}, nil, fileSearchTimeFormats["datetime"])
	if !strings.Contains(cappedEmpty, "候选池已达上限") {
		t.Errorf("空页触顶提示缺失: %q", cappedEmpty)
	}

	// 硬上限收敛提示
	clamped := formatFileSearchResult(`a`, fileSearchPage{Page: 1, PageSize: 20, Total: 500, MaxResultsClamped: true, More: true}, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(clamped, "超过硬上限 20") {
		t.Errorf("收敛提示缺失: %q", clamped)
	}

	// 页码超上限同样不静默
	pageClamped := formatFileSearchResult(`a`, fileSearchPage{Page: 100, PageSize: 10, Total: 5000, PageClamped: true, More: true}, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(pageClamped, "page 超过上限 100") {
		t.Errorf("页码收敛提示缺失: %q", pageClamped)
	}

	// 排序口径如实标注（显式 sort 不再误标"按相关性"）
	sorted := formatFileSearchResult(`a`, fileSearchPage{Page: 1, PageSize: 10, Total: 1, Order: fileSearchOrderDesc("date_modified", true)}, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(sorted, "按 date_modified 降序") || strings.Contains(sorted, "按相关性排序") {
		t.Errorf("排序口径标注错误: %q", sorted)
	}
	// 未指定 sort 时为本地相关性重排
	if rel := formatFileSearchResult(`a`, fileSearchPage{Page: 1, PageSize: 10, Total: 1, Order: fileSearchOrderDesc("", false)}, items, fileSearchTimeFormats["datetime"]); !strings.Contains(rel, "按相关性排序") {
		t.Errorf("默认排序口径标注错误: %q", rel)
	}
}

// TestFileSearchPageSlice 验证分页在过滤结果集上连续切片：不重复、不跳条、末页如实收尾。
func TestFileSearchPageSlice(t *testing.T) {
	items := make([]everything.Item, 25)
	for i := range items {
		items[i] = everything.Item{Name: fmt.Sprintf("f%02d.go", i), Path: `d:\code\`}
	}
	const total = 25

	var seen []string
	for page := 1; page <= 4; page++ {
		pageItems, info := fileSearchPageSlice(items, page, 10, fileSearchCandidates(page, 10), total)
		for _, it := range pageItems {
			seen = append(seen, it.Name)
		}
		switch page {
		case 1, 2:
			if len(pageItems) != 10 || !info.More {
				t.Errorf("第 %d 页应满页且还有下一页: len=%d more=%v", page, len(pageItems), info.More)
			}
		case 3:
			if len(pageItems) != 5 || info.More {
				t.Errorf("第 3 页应为末页: len=%d more=%v", len(pageItems), info.More)
			}
		case 4:
			if len(pageItems) != 0 || info.More {
				t.Errorf("第 4 页应为空页: len=%d more=%v", len(pageItems), info.More)
			}
		}
	}
	if len(seen) != 25 {
		t.Fatalf("翻页累计条数错误（重复或跳条）: %d", len(seen))
	}
	for i, name := range seen {
		if want := fmt.Sprintf("f%02d.go", i); name != want {
			t.Fatalf("第 %d 条顺序错乱: got %s want %s", i, name, want)
		}
	}
}

// TestFileSearchPageSlicePageGuard 页码越界（<1）防御性归一到第 1 页，不因负偏移切片 panic。
func TestFileSearchPageSlicePageGuard(t *testing.T) {
	items := []everything.Item{{Name: "a.go", Path: `d:\`}}
	pageItems, info := fileSearchPageSlice(items, 0, 10, 30, 1)
	if len(pageItems) != 1 || info.Page != 1 {
		t.Errorf("页码 <1 应归一到第 1 页: len=%d page=%d", len(pageItems), info.Page)
	}
}

// TestFileSearchCandidates 候选池随页码加深而增大，硬上限 600 收敛。
func TestFileSearchCandidates(t *testing.T) {
	cases := []struct{ page, pageSize, want int }{
		{1, 10, 30}, {2, 10, 60}, {20, 10, 600}, {21, 10, 600}, {100, 20, 600}, {1, 20, 60},
	}
	for _, c := range cases {
		if got := fileSearchCandidates(c.page, c.pageSize); got != c.want {
			t.Errorf("fileSearchCandidates(%d,%d) = %d, want %d", c.page, c.pageSize, got, c.want)
		}
	}
}

func TestFileSearchSortValidation(t *testing.T) {
	for _, sort := range []string{"name", "date_modified", "size", "path"} {
		if !fileSearchSorts[sort] {
			t.Errorf("sort %q 应合法", sort)
		}
	}
	for _, sort := range []string{"", "relevance", "modified"} {
		if sort != "" && fileSearchSorts[sort] {
			t.Errorf("sort %q 应非法", sort)
		}
	}
}
