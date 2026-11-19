package webfetch

import (
	"context"
	"errors"
	"testing"

	fetchcontent "websearch/pkg/fetch"

	engine "github.com/daidaiJ/go-webfetch"
)

type qualityEngine struct {
	stubEngine
	result *engine.FetchResult
	err    error
}

func (e *qualityEngine) Fetch(context.Context, string) (*engine.FetchResult, error) {
	return e.result, e.err
}

func TestFetchPreservesNotFound(t *testing.T) {
	f := &Fetcher{engine: &qualityEngine{err: &engine.NotFoundError{StatusCode: 404}}}
	_, err := f.Fetch(context.Background(), "https://example.com/missing")
	var missing *engine.NotFoundError
	if !errors.As(err, &missing) || missing.StatusCode != 404 {
		t.Fatalf("expected typed 404, got %v", err)
	}
}

func TestFetchRequiresBodyNotLength(t *testing.T) {
	for _, tt := range []struct {
		name    string
		result  *engine.FetchResult
		wantErr bool
	}{
		{"title only", &engine.FetchResult{Title: "SPA", Mode: "inline"}, true},
		{"whitespace", &engine.FetchResult{Title: "SPA", Mode: "inline", Markdown: " \n\t"}, true},
		{"title and skip navigation", &engine.FetchResult{Title: "Project", Mode: "inline", Markdown: "# Project\n\n[Skip to content](#VPContent)"}, true},
		{"navigation shell", &engine.FetchResult{Title: "App", Mode: "inline", Markdown: "# App\n\n[Home](/) [Docs](/docs) [Pricing](/pricing) [Login](/login)"}, true},
		{"short meaningful body", &engine.FetchResult{Title: "Status", Mode: "inline", Markdown: "Open"}, false},
		{"saved body", &engine.FetchResult{Title: "Article", Mode: "saved_to_file", FilePath: "/tmp/article.md", TotalChars: 1000}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{engine: &qualityEngine{result: tt.result}}
			_, err := f.Fetch(context.Background(), "https://example.com/page")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestFetchLowQualityErrorKeepsCausalChain(t *testing.T) {
	f := &Fetcher{engine: &qualityEngine{result: &engine.FetchResult{
		Title: "App", Mode: "inline",
		Markdown: "# App\n\n[Home](/) [Docs](/docs) [Pricing](/pricing)",
	}}}
	_, err := f.Fetch(context.Background(), "https://example.com/app")
	if !fetchcontent.IsLowQuality(err) {
		t.Fatalf("expected LowQualityError, got %v", err)
	}
}
