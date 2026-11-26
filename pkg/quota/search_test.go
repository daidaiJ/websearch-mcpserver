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

func TestTavilyKeyQuotaDoesNotSpendOtherKeysAllowance(t *testing.T) {
	for _, tt := range []struct {
		name  string
		usage string
	}{
		{"with account limit", `{"key":{"usage":0,"limit":1},"account":{"plan_usage":0,"plan_limit":100}}`},
		{"with key limit only", `{"key":{"usage":0,"limit":1}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tt.usage)
			}))
			t.Cleanup(endpoint.Close)
			conf := config.Config{Tavily: config.TavilyConfig{SKList: []string{"a", "b"}}}
			conf.Dashboard.Quotas.EnforceProviders = []string{"tavily"}
			service := newService(conf, endpoint.URL, endpoint.Client(), openSearchQuotaStore(t))
			ctx := context.Background()

			primeB, err := service.AdmitSearch(ctx, "tavily", "b")
			if err != nil {
				t.Fatal(err)
			}
			primeB(false)
			finishA, err := service.AdmitSearch(ctx, "tavily", "a")
			if err != nil {
				t.Fatal(err)
			}
			finishB, err := service.AdmitSearch(ctx, "tavily", "b")
			if err != nil {
				t.Fatalf("key B should have one search while key A is pending: %v", err)
			}
			_, err = service.AdmitSearch(ctx, "tavily", "b")
			requireSearchSkip(t, err, SkipOfficial)
			finishB(false)
			finishA(true)
			finishB, err = service.AdmitSearch(ctx, "tavily", "b")
			if err != nil {
				t.Fatalf("key B should have one search after key A is sent: %v", err)
			}
			finishB(true)
			_, err = service.AdmitSearch(ctx, "tavily", "b")
			requireSearchSkip(t, err, SkipOfficial)
		})
	}
}

func TestTavilyAccountQuotaReservesOtherKeysWhenAccountIdentityIsUnclear(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"key":{"usage":0,"limit":100},"account":{"plan_usage":0,"plan_limit":1}}`)
	}))
	t.Cleanup(endpoint.Close)
	conf := config.Config{Tavily: config.TavilyConfig{SKList: []string{"a", "b"}}}
	conf.Dashboard.Quotas.EnforceProviders = []string{"tavily"}
	service := newService(conf, endpoint.URL, endpoint.Client(), openSearchQuotaStore(t))
	ctx := context.Background()

	finishA, err := service.AdmitSearch(ctx, "tavily", "a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AdmitSearch(ctx, "tavily", "b")
	requireSearchSkip(t, err, SkipOfficial)
	finishA(false)
	finishB, err := service.AdmitSearch(ctx, "tavily", "b")
	if err != nil {
		t.Fatal(err)
	}
	finishB(true)
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

func TestSearchQuotaTracksUsageWhilePreviouslyEnforcedProviderIsDisabled(t *testing.T) {
	ctx := context.Background()
	conf := config.Config{Exa: config.ExaConfig{APIKey: "key"}}
	conf.Dashboard.Quotas = config.QuotasConfig{Limits: map[string]int{"exa": 1}, EnforceProviders: []string{"exa"}}
	store := openSearchQuotaStore(t)
	service := New(conf, store)
	if err := service.ResetProvider("exa"); err != nil {
		t.Fatal(err)
	}

	disabled := conf
	disabled.Dashboard.Quotas.EnforceProviders = nil
	service = New(disabled, store)
	for range 2 {
		finish, err := service.AdmitSearch(ctx, "exa", "key")
		if err != nil {
			t.Fatal(err)
		}
		finish(true)
	}

	_, err := New(conf, store).AdmitSearch(ctx, "exa", "key")
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
