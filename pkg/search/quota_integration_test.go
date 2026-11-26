package search

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"websearch/pkg/client"
	"websearch/pkg/config"
	"websearch/pkg/quota"
	"websearch/pkg/search/core"
	"websearch/pkg/telemetry"
)

type searchTransportFunc func(*http.Request) (*http.Response, error)

func (f searchTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConfiguredSearchQuotaSerializesPaidRequestsAndReset(t *testing.T) {
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "quota.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conf := config.Config{Mode: config.ModeExa, Exa: config.ExaConfig{APIKey: "test-key"}}
	conf.Dashboard.Quotas = config.QuotasConfig{Limits: map[string]int{"exa": 1}, EnforceProviders: []string{"exa"}}
	var outbound atomic.Int32
	original := client.DefaultClient
	t.Cleanup(func() { client.DefaultClient = original })
	client.DefaultClient = client.New(client.DefaultTimeout).SetTransport(searchTransportFunc(func(r *http.Request) (*http.Response, error) {
		outbound.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"results":[{"title":"Result","url":"https://example.org","text":"content"}]}`)), Request: r}, nil
	}))
	service := quota.New(conf, store)
	group, err := NewFromConfigWithQuota(conf, service)
	if err != nil {
		t.Fatal(err)
	}
	searcher := group.Primary.(core.ContextSearcher)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _ = searcher.SearchRawContext(context.Background(), "search") })
	}
	wg.Wait()
	if outbound.Load() != 1 {
		t.Fatalf("paid outbound requests = %d; want 1", outbound.Load())
	}
	if err := service.ResetProvider("exa"); err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.SearchRawContext(context.Background(), "search after reset"); err != nil {
		t.Fatal(err)
	}
	if outbound.Load() != 2 {
		t.Fatalf("paid outbound requests after reset = %d; want 2", outbound.Load())
	}
}

func TestSearchQuotaFallsBackInAPIPoolAndKeepsHealthyAttemptCounts(t *testing.T) {
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "quota.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	previousStore := telemetry.Default()
	telemetry.SetDefault(store)
	t.Cleanup(func() {
		telemetry.SetDefault(previousStore)
		store.Close()
	})
	conf := config.Config{
		Mode:      config.ModeApipool,
		Apipool:   config.ApipoolConfig{Engines: []string{"exa", "anysearch"}, Strategy: "priority"},
		Exa:       config.ExaConfig{APIKey: "exa-key"},
		Anysearch: config.AnysearchConfig{APIKey: "anysearch-key"},
	}
	conf.Dashboard.Quotas = config.QuotasConfig{Limits: map[string]int{"exa": 1}, EnforceProviders: []string{"exa"}}
	original := client.DefaultClient
	t.Cleanup(func() { client.DefaultClient = original })
	var exaCalls atomic.Int32
	client.DefaultClient = client.New(client.DefaultTimeout).SetTransport(searchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") == "Bearer exa-key" {
			exaCalls.Add(1)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"results":[{"title":"Fallback","url":"https://example.org","content":"content"}]}}`)), Request: r}, nil
	}))
	service := quota.New(conf, store)
	if err := service.SetProviderUsed("exa", 1); err != nil {
		t.Fatal(err)
	}
	group, err := NewFromConfigWithQuota(conf, service)
	if err != nil {
		t.Fatal(err)
	}
	ctx := telemetry.WithRequestID(context.Background(), "quota-fallback")
	results, err := group.Primary.(core.ContextSearcher).SearchRawContext(ctx, "search")
	if err != nil || len(results) != 1 || results[0].Title != "Fallback" {
		t.Fatalf("results = %v, err = %v", results, err)
	}
	if exaCalls.Load() != 0 {
		t.Fatalf("blocked provider made %d outbound requests", exaCalls.Load())
	}
	events, err := store.RequestEvents("quota-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Provider != "exa" || events[0].Outcome != "skipped" || events[1].Provider != "anysearch" || !events[1].Success {
		t.Fatalf("unexpected provider chain: %+v", events)
	}
	overview, err := store.Overview()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range overview.Providers {
		if provider.Name == "exa" {
			t.Fatal("quota skip was counted as an upstream provider outcome")
		}
	}
}
