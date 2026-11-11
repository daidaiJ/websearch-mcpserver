package mcpserver

import (
	"websearch/pkg/i18n"
	"websearch/pkg/search"
)

const (
	minSearchContentChars  = 200
	maxSearchContentChars  = 500000
	deepSearchContentChars = 24000
)

// clampContentBudget bounds a per-call content budget. Values below the floor
// are raised so a single result still carries a usable snippet.
func clampContentBudget(n int) int {
	switch {
	case n <= 0:
		return 0
	case n < minSearchContentChars:
		return minSearchContentChars
	case n > maxSearchContentChars:
		return maxSearchContentChars
	default:
		return n
	}
}

// applyContentBudget caps the total Content characters across results and
// returns a copy, leaving the cached raw results untouched. The budget is
// split evenly; results with less content keep their full text, so the actual
// total can be below the cap.
func applyContentBudget(results []search.SearchResult, budget int) []search.SearchResult {
	budget = clampContentBudget(budget)
	if budget == 0 || len(results) == 0 {
		return results
	}

	total := 0
	for _, result := range results {
		if !result.ContentSaved {
			total += len([]rune(result.Content))
		}
	}
	if total <= budget {
		return results
	}

	share := budget / len(results)
	if share < minSearchContentChars {
		share = minSearchContentChars
	}
	marker := i18n.T("tool.search.content_budget_truncated")

	out := make([]search.SearchResult, len(results))
	copy(out, results)
	for i := range out {
		if out[i].ContentSaved {
			continue
		}
		runes := []rune(out[i].Content)
		if len(runes) <= share {
			continue
		}
		out[i].Content = string(runes[:share]) + marker
	}
	return out
}
