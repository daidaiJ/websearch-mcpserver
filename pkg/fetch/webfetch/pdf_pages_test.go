package webfetch

import "testing"

func TestParsedPDFPageCount(t *testing.T) {
	tests := []struct {
		name      string
		pages     []int
		maxPages  int
		pageCount int
		want      int
	}{
		{"selected pages in range", []int{1, 3, 5}, 20, 10, 3},
		{"selected pages past end", []int{1, 8, 42}, 20, 10, 2},
		{"max pages truncates", nil, 3, 10, 3},
		{"max pages exceeds document", nil, 30, 10, 10},
		{"unbounded known document", nil, 0, 7, 7},
		{"unknown page count", nil, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsedPDFPageCount(tt.pages, tt.maxPages, tt.pageCount); got != tt.want {
				t.Fatalf("parsedPDFPageCount(%v, %d, %d) = %d, want %d", tt.pages, tt.maxPages, tt.pageCount, got, tt.want)
			}
		})
	}
}
