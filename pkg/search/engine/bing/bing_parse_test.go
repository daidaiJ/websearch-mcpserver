package bing

import (
	"strings"
	"testing"
)

// TestParseResultsContract pins the Bing result-page selectors and the
// redirect decoding. A search-page markup change should fail this test rather
// than silently returning zero results.
func TestParseResultsContract(t *testing.T) {
	e := &bingEngine{}
	html := `
<ol id="b_results">
  <li class="b_algo">
    <h2><a href="https://go.dev/doc/go1.26">Go 1.26 release notes</a></h2>
    <p>Go 1.26 <span class="algoSlug_icon">icon</span> release notes and changes.</p>
    <span class="news_dt">2026-02-01</span>
  </li>
  <li class="b_algo">
    <h2><a href="https://cn.bing.com/ck/a?!&amp;&amp;u=a1aHR0cHM6Ly9leGFtcGxlLmNvbS9wb3N0">Redirected result</a></h2>
    <p>Snippet</p>
  </li>
  <li class="b_algo"><p>No link, should be skipped</p></li>
</ol>`

	results := e.parseResults(html)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2: %+v", len(results), results)
	}
	if results[0].Title != "Go 1.26 release notes" {
		t.Fatalf("title = %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev/doc/go1.26" {
		t.Fatalf("url = %q", results[0].URL)
	}
	if strings.Contains(results[0].Content, "icon") {
		t.Fatalf("algoSlug_icon should be removed from content: %q", results[0].Content)
	}
	if !strings.Contains(results[0].Content, "release notes") {
		t.Fatalf("content = %q", results[0].Content)
	}
	if results[0].PublishedAt != "2026-02-01" {
		t.Fatalf("publishedAt = %q", results[0].PublishedAt)
	}
	if results[1].URL != "https://example.com/post" {
		t.Fatalf("redirect url = %q, want decoded target", results[1].URL)
	}
}
