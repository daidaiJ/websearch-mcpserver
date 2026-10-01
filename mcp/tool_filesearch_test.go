package mcpserver

import (
	"strings"
	"testing"
	"time"
	"websearch/pkg/fetch/everything"
)

func TestFormatFileSearchResultEmpty(t *testing.T) {
	out := formatFileSearchResult(`*.go`, 0, 0, nil, fileSearchTimeFormats["datetime"])
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

	out := formatFileSearchResult(`factory`, 1, 1, items, fileSearchTimeFormats["datetime"])
	for _, want := range []string{"factory.go", "d:\\code\\ai\\websearch\\pkg\\search\\", "17.0 KB", "2026-10-01 16:29:40"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q: %q", want, out)
		}
	}
	if !strings.Contains(out, "命中 1 条") {
		t.Errorf("总数行缺失: %q", out)
	}
}

func TestFormatFileSearchResultTruncated(t *testing.T) {
	items := []everything.Item{{Name: "a.go", Path: `d:\`}}
	out := formatFileSearchResult(`a`, 10, 3, items, fileSearchTimeFormats["datetime"])
	if !strings.Contains(out, "命中过多") {
		t.Errorf("截断提示缺失: %q", out)
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
