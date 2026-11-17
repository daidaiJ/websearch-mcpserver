package core

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeDomainPattern(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"example.com", "example.com", false},
		{" HTTPS://WWW.Example.COM:8443/path?q=1 ", "example.com", false},
		{"*.foo.example", "foo.example", false},
		{"example.com.", "example.com", false},
		{"//sub.example.com/x", "sub.example.com", false},
		{"example.com:8080", "example.com", false},
		{"https://user:pass@example.com/path", "example.com", false},
		{"*.example.com", "example.com", false},
		{"foo bar", "", true},
		{"example..com", "", true},
		{"*", "", true},
		{"http://", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeDomainPattern(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeDomainPattern(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeDomainPattern(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewDomainFilterNormalizesDeduplicatesAndSorts(t *testing.T) {
	filter, err := NewDomainFilter(
		[]string{"www.Example.com", "example.com", "*.foo.com", " ", "api.example.com"},
		[]string{"Ads.Example.com", "ads.example.com"},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantInclude := []string{"api.example.com", "example.com", "foo.com"}
	wantExclude := []string{"ads.example.com"}
	if !reflect.DeepEqual(filter.Include, wantInclude) {
		t.Fatalf("include = %v, want %v", filter.Include, wantInclude)
	}
	if !reflect.DeepEqual(filter.Exclude, wantExclude) {
		t.Fatalf("exclude = %v, want %v", filter.Exclude, wantExclude)
	}

	_, err = NewDomainFilter([]string{"not a host"}, nil)
	var perr *DomainPatternError
	if !errors.As(err, &perr) || perr.Pattern != "not a host" || !errors.Is(err, ErrDomainPatternInvalid) {
		t.Fatalf("invalid pattern error = %v, want DomainPatternError with input", err)
	}
}

func TestDomainFilterAllows(t *testing.T) {
	filter := DomainFilter{
		Include: []string{"example.com", "docs.test"},
		Exclude: []string{"ads.example.com"},
	}
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/a", true},
		{"https://www.example.com/a", true},
		{"https://news.example.com/a", true},
		{"https://ads.example.com/a", false},
		{"https://notexample.com/a", false},
		{"https://example.com.evil.com/a", false},
		{"https://docs.test/page", true},
		{"https://sub.docs.test/page", true},
		{"HTTPS://EXAMPLE.COM/A", true},
		{"https://example.com:8443/a", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := filter.Allows(tt.url); got != tt.want {
			t.Errorf("Allows(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}

	excludeOnly := DomainFilter{Exclude: []string{"example.com"}}
	if !excludeOnly.Allows("https://other.test/a") {
		t.Error("exclude-only filter should keep unrelated hosts")
	}
	if excludeOnly.Allows("https://sub.example.com/a") {
		t.Error("exclude should match subdomains")
	}
	if !excludeOnly.Allows("https://example.com.evil.test/a") {
		t.Error("exclude must not match a host that merely contains the pattern")
	}
	if !(DomainFilter{}).Allows("https://anything.test") {
		t.Error("empty filter should keep every result")
	}
	if !(DomainFilter{}).Allows("") {
		t.Error("empty filter should keep results without URLs")
	}
}

func TestFilterByDomain(t *testing.T) {
	results := []SearchResult{
		{Title: "keep", Url: "https://example.com/a"},
		{Title: "drop", Url: "https://ads.example.com/b"},
		{Title: "keep2", Url: "https://example.com/c"},
	}
	kept, dropped := FilterByDomain(results, DomainFilter{Exclude: []string{"ads.example.com"}})
	if dropped != 1 || len(kept) != 2 || kept[0].Title != "keep" || kept[1].Title != "keep2" {
		t.Fatalf("kept = %+v, dropped = %d", kept, dropped)
	}

	same, dropped := FilterByDomain(results, DomainFilter{})
	if dropped != 0 || len(same) != len(results) {
		t.Fatalf("empty filter changed results: %+v, %d", same, dropped)
	}
}

func TestDomainFilterContext(t *testing.T) {
	filter := DomainFilter{Include: []string{"example.com"}}
	ctx := WithDomainFilter(t.Context(), filter)
	if got := DomainFilterFromContext(ctx); !reflect.DeepEqual(got, filter) {
		t.Fatalf("context filter = %+v, want %+v", got, filter)
	}
	if got := DomainFilterFromContext(t.Context()); !got.Empty() {
		t.Fatalf("unexpected filter on bare context: %+v", got)
	}
	// Empty filters are not attached, keeping the context chain unchanged.
	ctx = WithDomainFilter(ctx, DomainFilter{})
	if got := DomainFilterFromContext(ctx); !reflect.DeepEqual(got, filter) {
		t.Fatalf("empty filter overwrote parent: %+v", got)
	}
}
