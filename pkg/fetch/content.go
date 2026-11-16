package fetch

import (
	"regexp"
	"strings"
)

var skipContentLink = regexp.MustCompile(`(?i)^\[(?:skip to content|skip to main content)\]\(#[^\s)]+\)$`)

// HasPageBody rejects an empty shell without imposing a minimum article length.
func HasPageBody(markdown, title string) bool {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		heading := strings.HasPrefix(line, "# ") && (strings.TrimSpace(title) == "" || line == "# "+strings.TrimSpace(title))
		if line == "" || heading || skipContentLink.MatchString(line) {
			continue
		}
		return true
	}
	return false
}
