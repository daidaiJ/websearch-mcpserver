package google

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"websearch/internal/testenv"

	"websearch/pkg/antirobot"

	"github.com/PuerkitoBio/goquery"
)

func TestBuildURL(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{}}

	tests := []struct {
		name      string
		query     string
		page      int
		timeRange antirobot.TimeRange
		wantQs    []string
		notWant   []string
	}{
		{
			name:      "basic query",
			query:     "golang concurrency",
			page:      1,
			timeRange: antirobot.TimeRangeNone,
			wantQs:    []string{"q=golang+concurrency", "hl=en", "ie=utf8", "oe=utf8", "filter=0"},
		},
		{
			name:      "page 2",
			query:     "test",
			page:      2,
			timeRange: antirobot.TimeRangeNone,
			wantQs:    []string{"start=10"},
		},
		{
			name:      "page 1 no start",
			query:     "test",
			page:      1,
			timeRange: antirobot.TimeRangeNone,
			notWant:   []string{"start="},
		},
		{
			name:      "time range day",
			query:     "news",
			page:      1,
			timeRange: antirobot.TimeRangeDay,
			wantQs:    []string{"tbs=qdr%3Ad"},
		},
		{
			name:      "time range week",
			query:     "news",
			page:      1,
			timeRange: antirobot.TimeRangeWeek,
			wantQs:    []string{"tbs=qdr%3Aw"},
		},
		{
			name:      "time range year",
			query:     "news",
			page:      1,
			timeRange: antirobot.TimeRangeYear,
			wantQs:    []string{"tbs=qdr%3Ay"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := e.buildURL(tt.query, tt.page, tt.timeRange)
			for _, qs := range tt.wantQs {
				if !strings.Contains(u, qs) {
					t.Errorf("URL missing %q\n  got: %s", qs, u)
				}
			}
			for _, qs := range tt.notWant {
				if strings.Contains(u, qs) {
					t.Errorf("URL should not contain %q\n  got: %s", qs, u)
				}
			}
			if !strings.HasPrefix(u, "https://www.google.com/search?") {
				t.Errorf("URL should start with google search endpoint, got: %s", u)
			}
		})
	}
}

func TestBuildURL_WithBlocked(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{Blocked: []string{"pinterest.com"}}}
	u := e.buildURL("design patterns", 1, antirobot.TimeRangeNone)
	if !strings.Contains(u, "-site") || !strings.Contains(u, "pinterest.com") {
		t.Errorf("URL should contain blocked site, got: %s", u)
	}
}

func TestParseResults(t *testing.T) {
	e := &googleEngine{}

	htmlText := `<!DOCTYPE html>
<html>
<body>
<div id="search">
  <div class="g">
    <div><a href="https://example.com/article1"><h3>First Result Title</h3></a></div>
    <div data-sncf="1">This is the content snippet for the first result.</div>
  </div>
  <div class="g">
    <div><a href="https://example.com/article2"><h3>Second Result</h3></a></div>
    <div class="VwiC3b">Content for the second result with <em>emphasis</em>.</div>
  </div>
  <div class="g">
    <div><a href="/url?q=https://redirected.com/page&amp;sa=U"><h3>Redirected Result</h3></a></div>
    <div class="IsZvec">Redirected content.</div>
  </div>
</div>
</body>
</html>`

	results := e.parseResults(htmlText)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	r := results[0]
	if r.Type != antirobot.ResultWeb {
		t.Errorf("type = %q, want web", r.Type)
	}
	if r.Title != "First Result Title" {
		t.Errorf("title = %q", r.Title)
	}
	if r.URL != "https://example.com/article1" {
		t.Errorf("url = %q", r.URL)
	}
	if !strings.Contains(r.Content, "content snippet") {
		t.Errorf("content = %q", r.Content)
	}
	if r.Engine != "google" {
		t.Errorf("engine = %q", r.Engine)
	}

	// 第二个结果
	r2 := results[1]
	if r2.URL != "https://example.com/article2" {
		t.Errorf("url = %q", r2.URL)
	}
	if !strings.Contains(r2.Content, "Content for") {
		t.Errorf("content = %q", r2.Content)
	}
}

func TestParseResults_RedirectURL(t *testing.T) {
	e := &googleEngine{}

	htmlText := `<html><body>
<div id="rso">
<div class="g">
  <div><a href="/url?q=https://real-site.com/article&amp;sa=U&amp;ved=xxx"><h3>Real Site</h3></a></div>
  <div data-sncf="1">Content here.</div>
</div>
</div>
</body></html>`

	results := e.parseResults(htmlText)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	if results[0].URL != "https://real-site.com/article" {
		t.Errorf("url = %q, want https://real-site.com/article", results[0].URL)
	}
}

func TestParseResults_Empty(t *testing.T) {
	e := &googleEngine{}

	tests := []struct {
		name string
		html string
	}{
		{"empty html", ""},
		{"no results", "<html><body><div>No results here</div></body></html>"},
		{"g without h3", `<html><body><div class="g"><div>no h3</div></div></body></html>`},
		{"g without href", `<html><body><div class="g"><h3>Title</h3></div></body></html>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := e.parseResults(tt.html)
			if len(results) != 0 {
				t.Errorf("expected 0 results, got %d", len(results))
			}
		})
	}
}

func TestExtractURL(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "direct http link",
			html: `<div class="g"><a href="https://example.com"><h3>T</h3></a></div>`,
			want: "https://example.com",
		},
		{
			name: "redirect format",
			html: `<div class="g"><a href="/url?q=https://real.com/page&amp;sa=U"><h3>T</h3></a></div>`,
			want: "https://real.com/page",
		},
		{
			name: "no url",
			html: `<div class="g"><h3>Title only</h3></div>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := newDoc(tt.html)
			if err != nil {
				t.Fatal(err)
			}
			sel := doc.Find("div.g")
			got := extractURL(sel)
			if got != tt.want {
				t.Errorf("extractURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractContent(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		wantSub string
	}{
		{
			name:    "data-sncf selector",
			html:    `<div class="g"><h3>T</h3><a href="https://x.com"></a><div data-sncf="1">Snippet text.</div></div>`,
			wantSub: "Snippet text",
		},
		{
			name:    "VwiC3b selector",
			html:    `<div class="g"><h3>T</h3><a href="https://x.com"></a><div class="VwiC3b">Another snippet.</div></div>`,
			wantSub: "Another snippet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := newDoc(tt.html)
			if err != nil {
				t.Fatal(err)
			}
			sel := doc.Find("div.g")
			got := extractContent(sel)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("extractContent() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestDetectSorry(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		code    int
		body    string
		wantErr bool
	}{
		{
			name:    "normal response",
			url:     "https://www.google.com/search?q=test",
			code:    200,
			body:    "<html><body>normal</body></html>",
			wantErr: false,
		},
		{
			name:    "sorry in path",
			url:     "https://www.google.com/sorry/index?continue=...",
			code:    200,
			body:    "",
			wantErr: true,
		},
		{
			name:    "302 redirect",
			url:     "https://www.google.com/search?q=test",
			code:    302,
			body:    "",
			wantErr: true,
		},
		{
			name:    "sorry in body",
			url:     "https://www.google.com/search?q=test",
			code:    200,
			body:    "<html><body>https://www.google.com/sorry/</body></html>",
			wantErr: true,
		},
		{
			name:    "large body with sorry",
			url:     "https://www.google.com/search?q=test",
			code:    200,
			body:    strings.Repeat("x", 3000) + "/sorry/",
			wantErr: false, // > 2000 bytes, not detected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.code,
				Request:    httptest.NewRequest("GET", tt.url, nil),
			}
			err := detectSorry(resp, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("detectSorry() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterBlocked(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{Blocked: []string{"pinterest.com"}}}

	results := []antirobot.Result{
		{URL: "https://www.pinterest.com/pin/123", Title: "Pinterest"},
		{URL: "https://de.pinterest.com/pin/456", Title: "Pinterest DE"},
		{URL: "https://valid-site.com/page", Title: "Valid"},
	}

	filtered := e.filterBlocked(results)
	if len(filtered) != 1 {
		t.Errorf("expected 1 result after filtering, got %d", len(filtered))
	}
	if len(filtered) > 0 && filtered[0].Title != "Valid" {
		t.Errorf("expected 'Valid', got %q", filtered[0].Title)
	}
}

func TestNewGoogle(t *testing.T) {
	e := NewGoogle(GoogleOpts{Enabled: true, ProxyResolve: func() string { return "http://127.0.0.1:7897" }})
	if e.Name() != "google" {
		t.Errorf("name = %q, want google", e.Name())
	}
	if e.Region() != antirobot.RegionInternational {
		t.Error("region should be International")
	}
}

func TestPreDelay(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{}}
	start := time.Now()
	e.preDelay()
	elapsed := time.Since(start)

	if elapsed < googleBaseDelay {
		t.Errorf("preDelay too short: %v < %v", elapsed, googleBaseDelay)
	}
}

func TestRecordFail_Backoff(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{}}

	e.recordFail()
	e.mu.Lock()
	bo1 := e.backoff
	e.mu.Unlock()
	if bo1 == 0 {
		t.Error("backoff should be > 0 after first fail")
	}

	e.recordFail()
	e.mu.Lock()
	bo2 := e.backoff
	e.mu.Unlock()
	if bo2 <= bo1 {
		t.Errorf("backoff should increase: %v <= %v", bo2, bo1)
	}
}

func TestRecordSuccess_Reset(t *testing.T) {
	e := &googleEngine{opts: GoogleOpts{}}
	e.recordFail()
	e.recordSuccess()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backoff != 0 {
		t.Errorf("backoff should be 0, got %v", e.backoff)
	}
	if e.consecFails != 0 {
		t.Errorf("consecFails should be 0, got %d", e.consecFails)
	}
}

func TestGoogleSearch(t *testing.T) {
	testenv.Require(t, testenv.Google)

	proxyEndpoint := "http://127.0.0.1:7897"
	engine := NewGoogle(GoogleOpts{
		Enabled:      true,
		ProxyResolve: func() string { return proxyEndpoint },
	})

	resp, err := engine.Search("golang concurrency", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search error: %v", err)
	}
	if resp.Engine != "google" {
		t.Errorf("engine = %q, want google", resp.Engine)
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

func TestGoogleSearch_NoProxy(t *testing.T) {
	testenv.Require(t, testenv.Google)

	// 无代理应超时或连接失败
	engine := NewGoogle(GoogleOpts{Enabled: true, ProxyResolve: nil})
	_, err := engine.Search("test", 1, antirobot.TimeRangeNone)
	// 无代理时 Google 在国内不可达，应该报错
	if err == nil {
		t.Log("Google search succeeded without proxy (might be on international network)")
	} else {
		t.Logf("Expected error without proxy: %v", err)
	}
}

// ── 辅助 ──

func newDoc(htmlStr string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
}
