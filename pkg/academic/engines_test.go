package academic

import (
	"strings"
	"testing"
	"websearch/internal/testenv"

	"websearch/pkg/antirobot"
)

func TestArxivSearch(t *testing.T) {
	testenv.Require(t, testenv.Arxiv)
	engine := NewArxiv(antirobot.ArxivOpts{Enabled: true}, nil)
	resp, err := engine.Search("diffusion probabilistic models", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search error: %v", err)
	}
	if resp.Engine != "arxiv" {
		t.Errorf("engine = %q, want arxiv", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results, got 0")
	}

	r := resp.Results[0]
	t.Logf("Title: %s", r.Title)
	t.Logf("URL: %s", r.URL)
	t.Logf("DOI: %s", r.DOI)
	t.Logf("Authors: %s", r.Authors)
	t.Logf("PDFURL: %s", r.PDFURL)
	t.Logf("PublishedAt: %s", r.PublishedAt)

	if r.Type != antirobot.ResultPaper {
		t.Errorf("type = %q, want paper", r.Type)
	}
	if r.Title == "" {
		t.Error("title is empty")
	}
	if !strings.HasPrefix(r.URL, "http") {
		t.Errorf("url = %q, should be a valid URL", r.URL)
	}
	if r.Authors == "" {
		t.Error("authors is empty")
	}
	if r.PDFURL == "" {
		t.Log("WARN: PDF URL is empty")
	}

	md := r.Markdown()
	t.Logf("\n--- Markdown ---\n%s", md)
	if !strings.Contains(md, r.Title) {
		t.Error("markdown missing title")
	}
}

func TestCrossrefSearch(t *testing.T) {
	testenv.Require(t, testenv.Crossref)
	engine := NewCrossref(antirobot.CrossrefOpts{Enabled: true}, nil)
	resp, err := engine.Search("graph neural network", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search error: %v", err)
	}
	if resp.Engine != "crossref" {
		t.Errorf("engine = %q, want crossref", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results, got 0")
	}

	r := resp.Results[0]
	t.Logf("Title: %s", r.Title)
	t.Logf("DOI: %s", r.DOI)
	t.Logf("Journal: %s", r.Journal)
	t.Logf("Authors: %s", r.Authors)
	t.Logf("PublishedAt: %s", r.PublishedAt)
	t.Logf("Score: %f", r.Score)

	if r.Type != antirobot.ResultPaper {
		t.Errorf("type = %q, want paper", r.Type)
	}
	if r.DOI == "" {
		t.Error("DOI is empty (Crossref should always have DOI)")
	}
	if r.Journal == "" {
		t.Log("WARN: journal is empty")
	}

	md := r.Markdown()
	t.Logf("\n--- Markdown ---\n%s", md)
	if r.DOI != "" && !strings.Contains(md, "DOI:") {
		t.Error("markdown missing DOI")
	}
}

func TestOpenAlexSearch(t *testing.T) {
	testenv.Require(t, testenv.OpenAlex)
	engine := NewOpenAlex(antirobot.OpenAlexOpts{Enabled: true}, nil)
	resp, err := engine.Search("reinforcement learning from human feedback", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("search error: %v", err)
	}
	if resp.Engine != "openalex" {
		t.Errorf("engine = %q, want openalex", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results, got 0")
	}

	r := resp.Results[0]
	t.Logf("Title: %s", r.Title)
	t.Logf("DOI: %s", r.DOI)
	t.Logf("Journal: %s", r.Journal)
	t.Logf("CitedBy: %d", r.CitedBy)
	t.Logf("Score: %f", r.Score)
	t.Logf("PDFURL: %s", r.PDFURL)

	if r.DOI == "" {
		t.Error("DOI is empty (OpenAlex should have DOI)")
	}
	if r.CitedBy == 0 {
		t.Log("WARN: cited_by is 0")
	}

	md := r.Markdown()
	t.Logf("\n--- Markdown ---\n%s", md)
	if r.DOI != "" && !strings.Contains(md, "DOI:") {
		t.Error("markdown missing DOI")
	}
	if r.CitedBy > 0 && !strings.Contains(md, "citations") {
		t.Error("markdown missing citations")
	}
}

func TestSemanticScholarSearch(t *testing.T) {
	engine := NewSemanticScholar(antirobot.SemanticScholarOpts{Enabled: true}, nil)
	resp, err := engine.Search("retrieval augmented generation", 1, antirobot.TimeRangeNone)
	if err != nil {
		t.Skipf("Semantic Scholar 不可达（国内网络预期行为）: %v", err)
	}
	if resp.Engine != "semantic_scholar" {
		t.Errorf("engine = %q, want semantic_scholar", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results, got 0")
	}

	r := resp.Results[0]
	t.Logf("Title: %s", r.Title)
	t.Logf("DOI: %s", r.DOI)
	t.Logf("Journal: %s", r.Journal)
	t.Logf("CitedBy: %d", r.CitedBy)
	t.Logf("Score: %f", r.Score)
	t.Logf("PDFURL: %s", r.PDFURL)

	if r.DOI == "" {
		t.Log("WARN: DOI is empty")
	}
	if r.CitedBy == 0 {
		t.Log("WARN: cited_by is 0")
	}

	md := r.Markdown()
	t.Logf("\n--- Markdown ---\n%s", md)
	if r.DOI != "" && !strings.Contains(md, "DOI:") {
		t.Error("markdown missing DOI")
	}
	if r.CitedBy > 0 && !strings.Contains(md, "citations") {
		t.Error("markdown missing citations")
	}
}
