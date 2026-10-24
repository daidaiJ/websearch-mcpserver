package baidu

import (
	"strings"
	"testing"
	"time"
	"websearch/internal/testenv"

	"websearch/pkg/antirobot"
)

func TestBuildURL(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{}}

	tests := []struct {
		name      string
		query     string
		page      int
		timeRange antirobot.TimeRange
		wantQs    []string
	}{
		{
			name:      "basic query",
			query:     "golang",
			page:      1,
			timeRange: antirobot.TimeRangeNone,
			wantQs:    []string{"wd=golang", "rn=10", "pn=0", "tn=json"},
		},
		{
			name:      "page 2",
			query:     "test",
			page:      2,
			timeRange: antirobot.TimeRangeNone,
			wantQs:    []string{"pn=10"},
		},
		{
			name:      "with blocked sites",
			query:     "test",
			page:      1,
			timeRange: antirobot.TimeRangeNone,
			wantQs:    []string{"wd=test"}, // -site: 会被 URL 编码
		},
		{
			name:      "time range day",
			query:     "news",
			page:      1,
			timeRange: antirobot.TimeRangeDay,
			wantQs:    []string{"gpc="}, // stf= 会被 URL 编码
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := e
			if tt.name == "with blocked sites" {
				engine = &baiduEngine{opts: BaiduOpts{Blocked: []string{"csdn.net"}}}
			}
			u := engine.buildURL(tt.query, tt.page, tt.timeRange)
			for _, qs := range tt.wantQs {
				if !strings.Contains(u, qs) {
					t.Errorf("URL missing %q\n  got: %s", qs, u)
				}
			}
			if !strings.HasPrefix(u, "https://www.baidu.com/s?") {
				t.Errorf("URL should start with baidu search endpoint, got: %s", u)
			}
		})
	}
}

func TestBaiduTimeRangeSeconds(t *testing.T) {
	tests := []struct {
		tr   antirobot.TimeRange
		want int
	}{
		{antirobot.TimeRangeNone, 0},
		{antirobot.TimeRangeDay, 86400},
		{antirobot.TimeRangeWeek, 604800},
		{antirobot.TimeRangeMonth, 2592000},
		{antirobot.TimeRangeYear, 31536000},
	}
	for _, tt := range tests {
		got := baiduTimeRangeSeconds(tt.tr)
		if got != tt.want {
			t.Errorf("baiduTimeRangeSeconds(%v) = %d, want %d", tt.tr, got, tt.want)
		}
	}
}

func TestParseResponse(t *testing.T) {
	e := &baiduEngine{}

	body := []byte(`{
		"feed": {
			"entry": [
				{
					"title": "Test Title &amp; More",
					"url": "https://example.com/1",
					"abs": "Test content &#39;quoted&#39;",
					"time": 1700000000
				},
				{
					"title": "Second Result",
					"url": "https://example.com/2",
					"abs": "Second content",
					"time": 0
				}
			]
		}
	}`)

	results, err := e.parseResponse(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	r := results[0]
	if r.Type != antirobot.ResultWeb {
		t.Errorf("type = %q, want web", r.Type)
	}
	if r.Title != "Test Title & More" {
		t.Errorf("title not unescaped: %q", r.Title)
	}
	if r.URL != "https://example.com/1" {
		t.Errorf("url = %q", r.URL)
	}
	if !strings.Contains(r.Content, "quoted") {
		t.Errorf("content not unescaped: %q", r.Content)
	}
	expectedDate := time.Unix(1700000000, 0).Format("2006-01-02")
	if r.PublishedAt != expectedDate {
		t.Errorf("publishedAt = %q, want %q", r.PublishedAt, expectedDate)
	}
	if r.Engine != "baidu_web" {
		t.Errorf("engine = %q", r.Engine)
	}

	r2 := results[1]
	if r2.PublishedAt != "" {
		t.Errorf("second result should have no date, got %q", r2.PublishedAt)
	}
}

func TestParseResponse_Empty(t *testing.T) {
	e := &baiduEngine{}

	body := []byte(`{"feed": {"entry": []}}`)
	_, err := e.parseResponse(body)
	if err == nil {
		t.Error("expected error for empty entry list")
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	e := &baiduEngine{}

	body := []byte(`not json at all`)
	_, err := e.parseResponse(body)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseResponse_MissingFields(t *testing.T) {
	e := &baiduEngine{}

	body := []byte(`{
		"feed": {
			"entry": [
				{"title": "", "url": "https://example.com", "abs": "content"},
				{"title": "Has Title", "url": "", "abs": "content"},
				{"title": "Valid", "url": "https://valid.com", "abs": "ok"}
			]
		}
	}`)

	results, err := e.parseResponse(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// 只有 title 和 url 都非空的条目才保留
	if len(results) != 1 {
		t.Errorf("expected 1 valid result, got %d", len(results))
	}
	if len(results) > 0 && results[0].Title != "Valid" {
		t.Errorf("title = %q, want Valid", results[0].Title)
	}
}

func TestFilterBlocked(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Blocked: []string{"csdn.net", "example.com"}}}

	results := []antirobot.Result{
		{URL: "https://www.csdn.net/article/1", Title: "CSDN"},
		{URL: "https://blog.csdn.net/post/2", Title: "CSDN Blog"},
		{URL: "https://valid-site.com/page", Title: "Valid"},
		{URL: "https://example.com/page", Title: "Example"},
	}

	filtered := e.filterBlocked(results)
	if len(filtered) != 1 {
		t.Errorf("expected 1 result after filtering, got %d", len(filtered))
	}
	if len(filtered) > 0 && filtered[0].Title != "Valid" {
		t.Errorf("expected 'Valid', got %q", filtered[0].Title)
	}
}

func TestApplyBlocked(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Blocked: []string{"csdn.net"}}}
	q := e.applyBlocked("golang tutorial")
	if !strings.Contains(q, "-site:csdn.net") {
		t.Errorf("query should contain -site:csdn.net, got %q", q)
	}
	if !strings.HasPrefix(q, "golang tutorial") {
		t.Errorf("query should start with original query, got %q", q)
	}
}

func TestApplyBlocked_TooMany(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{Blocked: []string{"a.com", "b.com", "c.com", "d.com", "e.com", "f.com"}}}
	q := e.applyBlocked("golang")
	if strings.Contains(q, "-site:") {
		t.Error("should not append blocked sites when > 5")
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://www.csdn.net/article/1", "csdn.net"},
		{"https://blog.csdn.net/post", "blog.csdn.net"},
		{"http://example.com", "example.com"},
		{"://invalid", ""},
	}
	for _, tt := range tests {
		got := extractHost(tt.raw)
		if got != tt.want {
			t.Errorf("extractHost(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestNewBaiduWeb(t *testing.T) {
	e := NewBaiduWeb(BaiduOpts{Enabled: true})
	if e.Name() != "baidu_web" {
		t.Errorf("name = %q, want baidu_web", e.Name())
	}
	if e.Region() != antirobot.RegionChina {
		t.Error("region should be China")
	}
}

func TestBaiduSearch(t *testing.T) {
	testenv.Require(t, testenv.Baidu)

	engine := NewBaiduWeb(BaiduOpts{Enabled: true})
	resp, err := engine.Search("golang concurrency", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search error: %v", err)
	}
	if resp.Engine != "baidu_web" {
		t.Errorf("engine = %q, want baidu_web", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results, got 0")
	}

	r := resp.Results[0]
	t.Logf("Title: %s", r.Title)
	t.Logf("URL: %s", r.URL)
	t.Logf("Content: %s", r.Content[:min(len(r.Content), 100)])

	if r.Type != antirobot.ResultWeb {
		t.Errorf("type = %q, want web", r.Type)
	}
	if r.Title == "" {
		t.Error("title is empty")
	}
	if !strings.HasPrefix(r.URL, "http") {
		t.Errorf("url = %q, should be a valid URL", r.URL)
	}
}

func TestBaiduSearch_TimeRange(t *testing.T) {
	testenv.Require(t, testenv.Baidu)

	engine := NewBaiduWeb(BaiduOpts{Enabled: true})

	// 测试时间范围过滤
	resp, err := engine.Search("人工智能", 1, antirobot.TimeRangeWeek)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search with time range error: %v", err)
	}
	t.Logf("Results with time range: %d", len(resp.Results))

	// 验证 URL 包含 gpc 参数（= 在 URL 中编码为 %3D）
	e := engine.(*baiduEngine)
	u := e.buildURL("人工智能", 1, antirobot.TimeRangeWeek)
	if !strings.Contains(u, "gpc=stf") {
		t.Errorf("URL should contain gpc parameter for time range, got: %s", u)
	}
}

func TestPreDelay(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{}}
	start := time.Now()
	e.preDelay()
	elapsed := time.Since(start)

	// 基础延迟至少 500ms
	if elapsed < baiduBaseDelay {
		t.Errorf("preDelay too short: %v < %v", elapsed, baiduBaseDelay)
	}
}

func TestRecordFail_Backoff(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{}}

	// 第一次失败
	e.recordFail()
	e.mu.Lock()
	bo1 := e.backoff
	e.mu.Unlock()
	if bo1 == 0 {
		t.Error("backoff should be > 0 after first fail")
	}

	// 第二次失败，退避时间应该更长
	e.recordFail()
	e.mu.Lock()
	bo2 := e.backoff
	e.mu.Unlock()
	if bo2 <= bo1 {
		t.Errorf("backoff should increase: %v <= %v", bo2, bo1)
	}
}

func TestRecordSuccess_ResetBackoff(t *testing.T) {
	e := &baiduEngine{opts: BaiduOpts{}}
	e.recordFail()
	e.recordSuccess()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 {
		t.Errorf("backoff should be 0 after success, got %v", e.backoff)
	}
	if e.consecFails != 0 {
		t.Errorf("consecFails should be 0 after success, got %d", e.consecFails)
	}
}
