package quota

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

func TestSearchQuotaUsesSelectedOfficialKeyAndReservesBeforeSending(t *testing.T) {
	var officialRequests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialRequests.Add(1)
		used := 1
		if r.Header.Get("Authorization") == "Bearer exhausted" {
			used = 2
		}
		fmt.Fprintf(w, `{"account":{"plan_usage":%d,"plan_limit":2}}`, used)
	}))
	t.Cleanup(endpoint.Close)
	conf := config.Config{Tavily: config.TavilyConfig{SKList: []string{"exhausted", "usable"}}}
	conf.Dashboard.Quotas.EnforceProviders = []string{"tavily"}
	store := openSearchQuotaStore(t)
	service := newService(conf, endpoint.URL, endpoint.Client(), store)
	ctx := context.Background()

	_, err := service.AdmitSearch(ctx, "tavily", "exhausted")
	requireSearchSkip(t, err, SkipOfficial)
	finish, err := service.AdmitSearch(ctx, "tavily", "usable")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AdmitSearch(ctx, "tavily", "usable")
	requireSearchSkip(t, err, SkipOfficial)
	finish(false)
	finish, err = service.AdmitSearch(ctx, "tavily", "usable")
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	_, err = service.AdmitSearch(ctx, "tavily", "usable")
	requireSearchSkip(t, err, SkipOfficial)
	if officialRequests.Load() != 2 {
		t.Fatalf("official queries = %d, want one per selected key", officialRequests.Load())
	}
}

func TestSearchQuotaRecoversAtTinyFishUTCMidnightWithoutQueryingFetch(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Query().Get("start_after") {
		case "2026-11-26T00:00:00Z":
			fmt.Fprint(w, `{"total":1}`)
		case "2026-11-27T00:00:00Z":
			fmt.Fprint(w, `{"total":0}`)
		default:
			t.Errorf("unexpected search-usage query %s", r.URL.RawQuery)
		}
	}))
	t.Cleanup(endpoint.Close)
	conf := config.Config{TinyFish: config.TinyFishConfig{APIKey: "key"}}
	conf.Dashboard.Quotas = config.QuotasConfig{Reset: "daily", Limits: map[string]int{"tinyfish": 2}, EnforceProviders: []string{"tinyfish"}}
	service := newService(conf, "", endpoint.Client(), openSearchQuotaStore(t))
	service.tinyfishSearchURL = endpoint.URL
	service.tinyfishFetchURL = endpoint.URL + "/unexpected-fetch"
	now := time.Date(2026, 11, 26, 23, 59, 59, 0, time.UTC)
	service.now = func() time.Time { return now }
	ctx := context.Background()
	finish, err := service.AdmitSearch(ctx, "tinyfish", "key")
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	_, err = service.AdmitSearch(ctx, "tinyfish", "key")
	requireSearchSkip(t, err, SkipOfficial)
	now = now.Add(time.Second)
	finish, err = service.AdmitSearch(ctx, "tinyfish", "key")
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if requests.Load() != 2 {
		t.Fatalf("usage queries = %d, want two search windows", requests.Load())
	}
}

func TestSearchQuotaSeedsExistingUsageAndAdjustmentOnce(t *testing.T) {
	conf := config.Config{Exa: config.ExaConfig{APIKey: "key"}}
	conf.Dashboard.Quotas = config.QuotasConfig{Reset: "daily", Limits: map[string]int{"exa": 2}, EnforceProviders: []string{"exa"}}
	store := openSearchQuotaStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := store.Record(telemetry.Event{Kind: "provider", Provider: "exa", Success: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetQuotaState("exa", conf.Dashboard.Quotas.PeriodStart(now).Unix(), 1); err != nil {
		t.Fatal(err)
	}
	service := New(conf, store)
	service.now = func() time.Time { return now }
	_, err := service.AdmitSearch(ctx, "exa", "key")
	requireSearchSkip(t, err, SkipLocalLimit)

	// After the window rolls, preserve the new durable quota across a reopen
	// rather than reimporting the previous observation.
	now = now.AddDate(0, 0, 1)
	finish, err := service.AdmitSearch(ctx, "exa", "key")
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	reopened := New(conf, store)
	reopened.now = func() time.Time { return now }
	if got := reopened.Get(ctx); len(got) == 0 {
		t.Fatal("expected quota view")
	}
	finish, err = reopened.AdmitSearch(ctx, "exa", "key")
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	_, err = reopened.AdmitSearch(ctx, "exa", "key")
	requireSearchSkip(t, err, SkipLocalLimit)
}

func openSearchQuotaStore(t *testing.T) *telemetry.Store {
	t.Helper()
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "quota.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func requireSearchSkip(t *testing.T, err error, reason string) {
	t.Helper()
	var skip *SkipError
	if !errors.As(err, &skip) || skip.Reason != reason {
		t.Fatalf("skip error = %v, want reason %s", err, reason)
	}
}
