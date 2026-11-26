package mcpserver

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search"
)

const budgetTestMarker = "\n…TRUNCATED…"

func budgetTestResults(n, chars int) []search.SearchResult {
	out := make([]search.SearchResult, n)
	body := strings.Repeat("a", chars)
	for i := range out {
		out[i] = search.SearchResult{
			Title:   fmt.Sprintf("title-%d", i+1),
			Url:     fmt.Sprintf("https://example.com/%d", i+1),
			Content: body,
		}
	}
	return out
}

func TestApplyContentBudgetKeepsTopResultsWhole(t *testing.T) {
	results := budgetTestResults(3, 1000)
	out := applyContentBudgetWithMarker(results, 2000, budgetTestMarker)

	if out[0].Content != results[0].Content {
		t.Fatalf("top result should stay whole, got %d chars", len([]rune(out[0].Content)))
	}
	if got := len([]rune(out[1].Content)); got != 800+len([]rune(budgetTestMarker)) {
		t.Fatalf("second result should get the remaining 800 chars plus marker, got %d", got)
	}
	if got := len([]rune(out[2].Content)); got != minSearchContentChars+len([]rune(budgetTestMarker)) {
		t.Fatalf("tail result should keep the 200-char floor plus marker, got %d", got)
	}
	if !strings.HasSuffix(out[1].Content, budgetTestMarker) || !strings.HasSuffix(out[2].Content, budgetTestMarker) {
		t.Fatal("truncated results must carry the marker")
	}
}

func TestApplyContentBudgetTinyBudgetFeedsTopFirst(t *testing.T) {
	results := budgetTestResults(2, 1000)
	out := applyContentBudgetWithMarker(results, 300, budgetTestMarker)

	if got := len([]rune(out[0].Content)); got != 300+len([]rune(budgetTestMarker)) {
		t.Fatalf("top result should take the whole tiny budget, got %d", got)
	}
	if out[1].Content != budgetTestMarker {
		t.Fatalf("tail result should degrade to the marker, got %q", out[1].Content)
	}
}

func TestApplyContentBudgetNoChangeUnderBudget(t *testing.T) {
	results := budgetTestResults(2, 100)
	out := applyContentBudgetWithMarker(results, 1000, budgetTestMarker)
	for i := range results {
		if out[i].Content != results[i].Content {
			t.Fatalf("result %d changed under budget", i)
		}
	}
}

func TestApplyContentBudgetLeavesSavedNotices(t *testing.T) {
	results := []search.SearchResult{
		{Content: strings.Repeat("a", 1000)},
		{Content: "saved notice", ContentSaved: true},
	}
	out := applyContentBudgetWithMarker(results, 300, budgetTestMarker)
	if out[1].Content != "saved notice" {
		t.Fatalf("ContentSaved notice must stay untouched, got %q", out[1].Content)
	}
}

func TestApplyContentBudgetZeroDisables(t *testing.T) {
	results := budgetTestResults(2, 1000)
	out := applyContentBudget(results, 0)
	if out[0].Content != results[0].Content || out[1].Content != results[1].Content {
		t.Fatal("zero budget must leave results untouched")
	}
}

func budgetTestFormatter(query string, results []search.SearchResult) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", query)
	for i, result := range results {
		fmt.Fprintf(&sb, "## %d\n%s\n%s\n%s\n", i+1, result.Title, result.Url, result.Content)
	}
	return sb.String(), nil
}

func TestDumpSearchResultsRankedInlineKeepsTopBody(t *testing.T) {
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 3000})
	results := budgetTestResults(3, 1000)
	full, err := budgetTestFormatter("q", results)
	if err != nil {
		t.Fatal(err)
	}
	notice, dumped := dumpSearchResultsIfOversizeWithFormatter("q", results, full, budgetTestFormatter)
	if !dumped {
		t.Fatal("oversize render should dump")
	}
	if !strings.Contains(notice, "内联保留 1 条完整正文") || !strings.Contains(notice, "截断 2 条") {
		t.Fatalf("ranked notice should report kept/truncated counts: %s", notice)
	}
	if strings.Count(notice, strings.Repeat("a", 1000)) != 1 {
		t.Fatalf("exactly one full body should stay inline: %s", notice)
	}
	if !strings.Contains(notice, "正文已省略") {
		t.Fatalf("tail entries should carry the omission marker: %s", notice)
	}
	if len([]rune(notice)) > 3000 {
		t.Fatalf("ranked response exceeds the inline limit: %d", len([]rune(notice)))
	}
	_, rest, _ := strings.Cut(notice, "**文件路径**: `")
	path, _, _ := strings.Cut(rest, "`")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "a") < 3000 {
		t.Fatalf("dump file lost body content: %d chars", strings.Count(string(data), "a"))
	}
}

func TestDumpSearchResultsFallsBackWhenFormatterIgnoresResults(t *testing.T) {
	setDumpTestEnv(t, t.TempDir(), config.SmartSearchConfig{InlineMaxChars: 1000})
	results := budgetTestResults(3, 1000)
	ignoring := func(string, []search.SearchResult) (string, error) {
		return strings.Repeat("x", 5000), nil
	}
	notice, dumped := dumpSearchResultsIfOversizeWithFormatter("q", results, strings.Repeat("x", 5000), ignoring)
	if !dumped {
		t.Fatal("oversize render should dump")
	}
	if !strings.Contains(notice, "已整体保存到文件") {
		t.Fatalf("fallback should use the whole-response notice: %s", notice)
	}
	if strings.Contains(notice, strings.Repeat("a", 1000)) {
		t.Fatal("fallback preview must not inline bodies")
	}
}
