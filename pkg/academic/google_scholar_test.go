package academic

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"websearch/pkg/antirobot"
)

func TestScholarChallengeResponse(t *testing.T) {
	respWithURL := func(raw string) *http.Response {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{Request: &http.Request{URL: u}}
	}

	tests := []struct {
		name string
		resp *http.Response
		body string
		want bool
	}{
		{
			name: "sgsync final URL",
			resp: respWithURL("https://www.google.com/sgsync?url=https%3A%2F%2Fscholar.google.com&sg_fe=scife"),
			body: `<html><body>challenge</body></html>`,
			want: true,
		},
		{
			name: "verifying page title",
			resp: respWithURL("https://scholar.google.com/scholar?q=x"),
			body: `<html><head><title>Verifying...</title></head></html>`,
			want: true,
		},
		{
			name: "captcha form is handled by parser",
			resp: respWithURL("https://scholar.google.com/scholar?q=x"),
			body: `<form id="gs_captcha_f"></form>`,
			want: false,
		},
		{
			name: "normal results",
			resp: respWithURL("https://scholar.google.com/scholar?q=x"),
			body: `<div data-rp="1"><h3><a>Paper</a></h3></div>`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scholarChallengeResponse(tt.resp, []byte(tt.body)); got != tt.want {
				t.Fatalf("scholarChallengeResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGoogleScholarSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test")
	}
	engine := NewGoogleScholar(antirobot.GoogleScholarOpts{Enabled: true}, nil)
	resp, err := engine.Search("chain of thought prompting", 1, antirobot.TimeRangeNone)
	if err != nil {
		t.Skipf("Google Scholar 不可达（国内网络预期行为）: %v", err)
	}
	if resp.Engine != "google_scholar" {
		t.Errorf("engine = %q, want google_scholar", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results, got 0")
	}

	r := resp.Results[0]
	t.Logf("Title: %s", r.Title)
	t.Logf("URL: %s", r.URL)
	t.Logf("Authors: %s", r.Authors)
	t.Logf("Journal: %s", r.Journal)
	t.Logf("PublishedAt: %s", r.PublishedAt)
	t.Logf("CitedBy: %d", r.CitedBy)
	t.Logf("PDFURL: %s", r.PDFURL)

	if r.Type != antirobot.ResultPaper {
		t.Errorf("type = %q, want paper", r.Type)
	}
	if r.Title == "" {
		t.Error("title is empty")
	}
	if r.Authors == "" {
		t.Error("authors is empty")
	}

	// 验证 markdown 格式
	md := r.Markdown()
	t.Logf("\n--- Markdown Output ---\n%s", md)
	if !strings.Contains(md, r.Title) {
		t.Error("markdown missing title")
	}
	if r.CitedBy > 0 && !strings.Contains(md, strconv.Itoa(r.CitedBy)) {
		t.Error("markdown missing cited_by count")
	}
}

// TestParseScholarHTML_DOI GS 无原生 DOI 字段，落地页 URL 或摘要中含 DOI 时应补全。
func TestParseScholarHTML_DOI(t *testing.T) {
	html := `<div data-rp="1">
		<h3><a href="https://www.nature.com/articles/10.1038/nature12373">Paper From Publisher</a></h3>
		<div class="gs_a">A Author - Nature, 2013 - nature.com</div>
		<div class="gs_rs">Some abstract text without DOI reference.</div>
	</div>
	<div data-rp="2">
		<h3><a href="https://example.com/articles/second-paper">Paper From Abstract</a></h3>
		<div class="gs_a">B Author - Springer, 2014 - springer.com</div>
		<div class="gs_rs">This work extends 10.1007/978-3-319-00000-0_2 as cited before.</div>
	</div>
	<div data-rp="3">
		<h3><a href="https://arxiv.org/abs/1706.03762">No DOI Anywhere</a></h3>
		<div class="gs_a">C Author - arXiv, 2017 - arxiv.org</div>
		<div class="gs_rs">Plain preprint abstract.</div>
	</div>`

	resp, err := parseScholarHTML([]byte(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("want 3 results, got %d", len(resp.Results))
	}
	if got := resp.Results[0].DOI; got != "10.1038/nature12373" {
		t.Errorf("落地页 DOI = %q", got)
	}
	if got := resp.Results[1].DOI; got != "10.1007/978-3-319-00000-0_2" {
		t.Errorf("摘要内文 DOI = %q", got)
	}
	if got := resp.Results[2].DOI; got != "" {
		t.Errorf("无 DOI 来源应为空, got %q", got)
	}
}

func TestParseScholarMeta(t *testing.T) {
	tests := []struct {
		input        string
		wantAuthors  string
		wantJournal  string
		wantPubDate  string
		wantPubisher string
	}{
		{
			input:        "A Vaswani, N Shazeer, N Parmar - Advances in neural …, 2017 - proceedings.neurips.cc",
			wantAuthors:  "A Vaswani, N Shazeer, N Parmar",
			wantJournal:  "Advances in neural …",
			wantPubDate:  "2017",
			wantPubisher: "proceedings.neurips.cc",
		},
		{
			input:        "J Devlin, MW Chang, K Lee - arXiv preprint arXiv …, 2018 - arxiv.org",
			wantAuthors:  "J Devlin, MW Chang, K Lee",
			wantJournal:  "arXiv preprint arXiv …",
			wantPubDate:  "2018",
			wantPubisher: "arxiv.org",
		},
		{
			input:        "A Author - Some Publisher",
			wantAuthors:  "A Author",
			wantJournal:  "",
			wantPubDate:  "",
			wantPubisher: "Some Publisher",
		},
		{
			input:       "",
			wantAuthors: "",
		},
	}

	for _, tt := range tests {
		authors, journal, publisher, pubDate := parseScholarMeta(tt.input)
		if authors != tt.wantAuthors {
			t.Errorf("parseScholarMeta(%q): authors = %q, want %q", tt.input, authors, tt.wantAuthors)
		}
		if journal != tt.wantJournal {
			t.Errorf("parseScholarMeta(%q): journal = %q, want %q", tt.input, journal, tt.wantJournal)
		}
		if pubDate != tt.wantPubDate {
			t.Errorf("parseScholarMeta(%q): pubDate = %q, want %q", tt.input, pubDate, tt.wantPubDate)
		}
		if publisher != tt.wantPubisher {
			t.Errorf("parseScholarMeta(%q): publisher = %q, want %q", tt.input, publisher, tt.wantPubisher)
		}
	}
}

func TestParseCitedByText(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Cited by 1234", 1234},
		{"Cited by 1", 1},
		{"", 0},
		{"Cited by ", 0},
	}
	for _, tt := range tests {
		got := parseCitedByText(tt.input)
		if got != tt.want {
			t.Errorf("parseCitedByText(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
