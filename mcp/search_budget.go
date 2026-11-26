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
// returns a copy, leaving the cached raw results untouched.
//
// The budget is allocated in result order: earlier results may consume what
// they need, while a per-result floor is reserved for the entries behind them.
// This keeps the highest-ranked bodies intact when the budget is tight; tail
// entries degrade to a truncation marker instead of every result losing an
// equal share. Results with ContentSaved carry a transient file notice and are
// left untouched.
func applyContentBudget(results []search.SearchResult, budget int) []search.SearchResult {
	return applyContentBudgetWithMarker(results, budget, i18n.T("tool.search.content_budget_truncated"))
}

func applyContentBudgetWithMarker(results []search.SearchResult, budget int, marker string) []search.SearchResult {
	budget = clampContentBudget(budget)
	if budget == 0 || len(results) == 0 {
		return results
	}

	total := searchContentChars(results)
	if total <= budget {
		return results
	}

	out := make([]search.SearchResult, len(results))
	copy(out, results)

	// Reserve a readable snippet for every remaining entry when the budget can
	// afford it. With a very small budget the floor is dropped so the top result
	// still gets the space.
	floor := minSearchContentChars
	if budget < floor*countContentResults(out) {
		floor = 0
	}

	remaining := budget
	for i := range out {
		if out[i].ContentSaved {
			continue
		}
		runes := []rune(out[i].Content)
		if len(runes) == 0 {
			continue
		}

		reserve := 0
		if floor > 0 {
			for j := i + 1; j < len(out); j++ {
				if !out[j].ContentSaved && len([]rune(out[j].Content)) > 0 {
					reserve += floor
				}
			}
		}

		allowance := remaining - reserve
		if allowance < 0 {
			allowance = 0
		}
		take := len(runes)
		if take > allowance {
			take = allowance
		}
		if take < len(runes) {
			out[i].Content = string(runes[:take]) + marker
		}
		remaining -= take
		if remaining < 0 {
			remaining = 0
		}
	}
	return out
}

// searchContentChars counts document body characters, excluding transient
// ContentSaved notices.
func searchContentChars(results []search.SearchResult) int {
	total := 0
	for _, result := range results {
		if result.ContentSaved {
			continue
		}
		total += len([]rune(result.Content))
	}
	return total
}

func countContentResults(results []search.SearchResult) int {
	n := 0
	for _, result := range results {
		if !result.ContentSaved && len([]rune(result.Content)) > 0 {
			n++
		}
	}
	return n
}
