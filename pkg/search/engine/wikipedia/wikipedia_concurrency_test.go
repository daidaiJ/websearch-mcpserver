package wikipedia

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"websearch/pkg/antirobot"
)

type hangRT struct{}

func (hangRT) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestWikipediaContextCancelStopsRequest(t *testing.T) {
	e := NewWikipedia(WikipediaOpts{Enabled: true, PerSec: 1000, PerMin: 10000}).(*wikipediaEngine)
	e.client = &http.Client{Transport: hangRT{}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := e.SearchContext(ctx, "q", 1, antirobot.TimeRangeNone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SearchContext err = %v, want context deadline exceeded", err)
	}
}
