package bing

import (
	"strings"
	"testing"
	"websearch/internal/testenv"

	"websearch/pkg/antirobot"
)

func TestBingSearch(t *testing.T) {
	testenv.Require(t, testenv.Bing)
	engine := NewBing(BingOpts{Enabled: true})
	resp, err := engine.Search("golang concurrency", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search error: %v", err)
	}
	if resp.Engine != "bing" {
		t.Errorf("engine = %q, want bing", resp.Engine)
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
	if r.DOI != "" {
		t.Errorf("web result should not have DOI, got %q", r.DOI)
	}
	if r.Journal != "" {
		t.Errorf("web result should not have journal, got %q", r.Journal)
	}
}

func TestMergeBlocked(t *testing.T) {
	merged := MergeBlocked(
		[]string{"example.com", "test.org"},
		[]string{"test.org", "blocked.net"},
	)
	seen := make(map[string]bool)
	for _, d := range merged {
		if seen[d] {
			t.Errorf("duplicate domain: %s", d)
		}
		seen[d] = true
	}
	if len(merged) != 3 {
		t.Errorf("expected 3 unique domains, got %d: %v", len(merged), merged)
	}
}
