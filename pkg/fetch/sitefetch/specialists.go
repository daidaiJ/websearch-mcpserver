package sitefetch

import "context"

// SpecialistFetcher is a site-specific reader that replaces the generic
// HTML-to-Markdown path for one URL family.
type SpecialistFetcher interface {
	Match(rawURL string) bool
	Fetch(ctx context.Context, rawURL string) (string, error)
	Provider() string
}

var defaultSpecialists = []SpecialistFetcher{
	NewStackExchangeFetcher(),
	NewWikipediaFetcher(),
	NewArxivFetcher(),
}

// MatchSpecialist reports which specialist would handle rawURL without making
// a request. It keeps route tests and diagnostics network-free.
func MatchSpecialist(rawURL string) (provider string, matched bool) {
	for _, fetcher := range defaultSpecialists {
		if fetcher.Match(rawURL) {
			return fetcher.Provider(), true
		}
	}
	return "", false
}

// FetchSpecialist routes a URL to the first matching specialist. matched=false
// means the URL belongs on the generic fallback chain; a non-nil err from a
// matched specialist is also recoverable through that chain.
func FetchSpecialist(ctx context.Context, rawURL string) (text, provider string, matched bool, err error) {
	for _, fetcher := range defaultSpecialists {
		if !fetcher.Match(rawURL) {
			continue
		}
		text, err = fetcher.Fetch(ctx, rawURL)
		return text, fetcher.Provider(), true, err
	}
	return "", "", false, nil
}
