package mcphub

import (
	"strings"
	"testing"

	"websearch/pkg/search/core"
)

func TestParseParallelSearch(t *testing.T) {
	res := toolResult{Structured: map[string]any{
		"results": []any{
			map[string]any{
				"url":          "https://example.com/a",
				"title":        "A",
				"publish_date": "2026-10-01",
				"excerpts":     []any{"first", "second"},
			},
			map[string]any{
				"url":          "https://example.com/b",
				"title":        "B",
				"publish_date": nil,
				"excerpts":     []any{"third"},
			},
		},
	}}
	got, err := parseSearch("parallel", res, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d", len(got))
	}
	if got[0].Content != "first\n\nsecond" || got[0].PublishDate != "2026-10-01" || got[0].DateSource != core.DateSourceStructured {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].PublishDate != "" || got[1].DateSource != "" {
		t.Fatalf("null publish date must stay empty: %+v", got[1])
	}
}

func TestParseParallelSearchTextFallback(t *testing.T) {
	res := toolResult{Text: `{"results":[{"url":"https://example.com","title":"T","excerpts":["x"]}]}`}
	got, err := parseSearch("parallel", res, 1)
	if err != nil || len(got) != 1 || got[0].Url != "https://example.com" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestParseFirecrawlSearch(t *testing.T) {
	res := toolResult{Structured: map[string]any{
		"data": map[string]any{"web": []any{
			map[string]any{
				"url":         "https://example.com/f",
				"title":       "F",
				"description": "body",
				"metadata":    map[string]any{"publishedDate": "2026-10-02"},
			},
		}},
	}}
	got, err := parseSearch("firecrawl", res, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got[0].Content != "body" || got[0].PublishDate != "2026-10-02" || got[0].Engine != "firecrawl_mcp" {
		t.Fatalf("result = %+v", got[0])
	}
}

func TestParseExaSearch(t *testing.T) {
	text := `Title: First
URL: https://example.com/1
Published: 2026-10-03T10:00:00Z
Author: N/A
Highlights:
line one
line two

Title: Second
URL: https://example.com/2
Published: N/A
Author: someone
Highlights:
second body`
	got, err := parseSearch("exa", toolResult{Text: text}, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got[0].Title != "First" || got[0].Url != "https://example.com/1" || !strings.Contains(got[0].Content, "line one") {
		t.Fatalf("first = %+v", got[0])
	}
	if got[0].PublishDate != "2026-10-03T10:00:00Z" || got[1].PublishDate != "" {
		t.Fatalf("dates = %q / %q", got[0].PublishDate, got[1].PublishDate)
	}
}

func TestParseAnysearchSearch(t *testing.T) {
	text := `## Search Results (2 results, 120ms)

### 1. 国内 AI 新闻
- **URL**: https://example.cn/news
- **Published**: 2026-10-04
- 第一段
- 第二段

### 2. Another
- **URL**: https://example.com/a
- body`
	got, err := parseSearch("anysearch", toolResult{Text: text}, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got[0].Title != "国内 AI 新闻" || got[0].Url != "https://example.cn/news" || got[0].PublishDate != "2026-10-04" {
		t.Fatalf("result = %+v", got[0])
	}
	if !strings.Contains(got[0].Content, "第一段") || !strings.Contains(got[0].Content, "第二段") {
		t.Fatalf("content = %q", got[0].Content)
	}
}

func TestParseFetches(t *testing.T) {
	parallel := toolResult{Structured: map[string]any{
		"results": []any{map[string]any{
			"url":          "https://example.com",
			"title":        "Example",
			"full_content": "full body",
		}},
	}}
	text, err := parseFetch("parallel", parallel)
	if err != nil || !strings.Contains(text, "full body") || !strings.Contains(text, "# Example") {
		t.Fatalf("parallel text=%q err=%v", text, err)
	}

	firecrawl := toolResult{Structured: map[string]any{
		"markdown": "body",
		"metadata": map[string]any{"title": "Fire"},
	}}
	text, err = parseFetch("firecrawl", firecrawl)
	if err != nil || !strings.Contains(text, "# Fire") || !strings.Contains(text, "body") {
		t.Fatalf("firecrawl text=%q err=%v", text, err)
	}

	text, err = parseFetch("exa", toolResult{Text: "# Ex\nURL: https://example.com\n\nbody"})
	if err != nil || strings.Contains(text, "URL: ") || !strings.Contains(text, "body") {
		t.Fatalf("exa text=%q err=%v", text, err)
	}

	anysearch := toolResult{Structured: map[string]any{
		"title": "Any", "content": "body",
	}}
	text, err = parseFetch("anysearch", anysearch)
	if err != nil || !strings.Contains(text, "# Any") || !strings.Contains(text, "body") {
		t.Fatalf("anysearch text=%q err=%v", text, err)
	}
}
