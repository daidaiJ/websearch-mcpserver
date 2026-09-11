package academic

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"websearch/pkg/antirobot"
)

func TestCrossrefTimeRangeUsesFilterParameter(t *testing.T) {
	var got *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"items":[]}}`))
	}))
	t.Cleanup(ts.Close)

	oldPath := crossrefPath
	crossrefPath = ts.URL
	t.Cleanup(func() { crossrefPath = oldPath })

	engine := NewCrossref(antirobot.CrossrefOpts{Enabled: true}, nil).(*crossrefEngine)
	if _, err := engine.Search("machine learning", 1, antirobot.TimeRangeYear); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("request was not captured")
	}
	query := got.URL.Query()
	wantFilter := "from-pub-date:" + antirobot.TimeRangeSince(antirobot.TimeRangeYear)
	if query.Get("filter") != wantFilter {
		t.Fatalf("filter = %q, want %q", query.Get("filter"), wantFilter)
	}
	if query.Get("from-pub-date") != "" {
		t.Fatalf("unexpected top-level from-pub-date: %q", query.Get("from-pub-date"))
	}
}

func TestDOAJTimeRangeUsesYearRangeWithoutWildcard(t *testing.T) {
	var escapedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	t.Cleanup(ts.Close)

	oldPath := doajPath
	doajPath = ts.URL
	t.Cleanup(func() { doajPath = oldPath })

	engine := NewDOAJ(antirobot.DOAJOpts{Enabled: true}, nil).(*doajEngine)
	if _, err := engine.Search("machine learning", 1, antirobot.TimeRangeYear); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil {
		t.Fatalf("unescape path: %v", err)
	}
	startYear := antirobot.TimeRangeSince(antirobot.TimeRangeYear)[:4]
	if !strings.Contains(decoded, "bibjson.year:["+startYear+" TO ") {
		t.Fatalf("decoded path lacks explicit bibjson.year range: %q", decoded)
	}
	if strings.Contains(decoded, "*") {
		t.Fatalf("DOAJ wildcard must not be used: %q", decoded)
	}
	if !strings.Contains(escapedPath, "%20") {
		t.Fatalf("DOAJ path segment must encode spaces as %%20: %q", escapedPath)
	}
}

func TestArxivTimeRangeUsesExplicitTimestampRange(t *testing.T) {
	var searchQuery string
	withArxivTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		searchQuery = r.URL.Query().Get("search_query")
		_, _ = w.Write([]byte(okArxivXML))
	})

	engine := arxivTestEngine()
	if _, err := engine.Search("machine learning", 1, antirobot.TimeRangeYear); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(searchQuery, "*") {
		t.Fatalf("arXiv wildcard must not be used: %q", searchQuery)
	}
	start := strings.Index(searchQuery, "submittedDate:[")
	if start < 0 {
		t.Fatalf("missing submittedDate range: %q", searchQuery)
	}
	rangeText := searchQuery[start+len("submittedDate:["):]
	end := strings.Index(rangeText, "]")
	if end < 0 {
		t.Fatalf("unterminated submittedDate range: %q", searchQuery)
	}
	parts := strings.Split(rangeText[:end], " TO ")
	if len(parts) != 2 || len(parts[0]) != 12 || len(parts[1]) != 12 {
		t.Fatalf("submittedDate range must use two 12-digit timestamps: %q", rangeText[:end])
	}
}
