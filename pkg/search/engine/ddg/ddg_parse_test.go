package ddg

import "testing"

// TestParseResultsContract pins the DuckDuckGo result-page selectors, ad
// filtering and redirect decoding. A markup change should fail here instead of
// silently returning zero results.
func TestParseResultsContract(t *testing.T) {
	e := &ddgEngine{}
	html := `
<div class="result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Fgo1.26&amp;rut=abc">Go 1.26 release notes</a>
  <div class="result__snippet">Go 1.26 release notes and changes.</div>
  <div class="result__url">go.dev</div>
</div>
<div class="result result--ad">
  <a class="result__a" href="https://ads.example.com/">Sponsored</a>
  <div class="result__snippet">Ad</div>
</div>
<div class="result">
  <a class="result__a" href="https://example.com/direct">Direct result</a>
</div>`

	results := e.parseResults(html)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2: %+v", len(results), results)
	}
	if results[0].Title != "Go 1.26 release notes" {
		t.Fatalf("title = %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev/doc/go1.26" {
		t.Fatalf("redirect url = %q, want decoded target", results[0].URL)
	}
	if results[0].Content == "" {
		t.Fatal("snippet should be captured")
	}
	if results[1].URL != "https://example.com/direct" {
		t.Fatalf("direct url = %q", results[1].URL)
	}
}
