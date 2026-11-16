package fetch

import "testing"

func TestHasPageBody(t *testing.T) {
	for _, tt := range []struct {
		name, markdown, title string
		want                  bool
	}{
		{"empty", " \n\t", "SPA", false},
		{"title", "# SPA", "SPA", false},
		{"title without metadata", "# SPA", "", false},
		{"title and navigation without metadata", "# Project\n[Skip to content](#main)", "", false},
		{"skip navigation", "[Skip to content](#VPContent)", "Project", false},
		{"title and skip", "# Project\n\n[Skip to content](#main)", "Project", false},
		{"short body", "Open", "Status", true},
		{"body after title", "# Project\nOpen", "Project", true},
		{"short body without metadata", "# Project\nOpen", "", true},
		{"other fragment link", "[References](#references)", "Index", true},
		{"external link", "[Skip to content](https://example.com/)", "Links", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPageBody(tt.markdown, tt.title); got != tt.want {
				t.Fatalf("has body = %v, want %v", got, tt.want)
			}
		})
	}
}
