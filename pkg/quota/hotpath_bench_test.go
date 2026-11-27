package quota

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

func openBenchStore(b *testing.B) *telemetry.Store {
	b.Helper()
	store, err := telemetry.Open(filepath.Join(b.TempDir(), "quota-bench.db"), 30)
	if err != nil {
		b.Fatal(err)
	}
	return store
}

// BenchmarkAdmitSearchStaleSnapshot measures the search admission cost when
// the cached official usage snapshot is older than searchQuotaTTL and the
// upstream usage endpoint takes 200ms.
func BenchmarkAdmitSearchStaleSnapshot(b *testing.B) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `{"account":{"plan_usage":0,"plan_limit":1000}}`)
	}))
	defer endpoint.Close()

	conf := config.Config{Tavily: config.TavilyConfig{SKList: []string{"key"}}}
	conf.Dashboard.Quotas.EnforceProviders = []string{"tavily"}
	store := openBenchStore(b)
	defer store.Close()
	service := newService(conf, endpoint.URL, endpoint.Client(), store)
	now := time.Now()
	service.now = func() time.Time { return now }
	ctx := context.Background()

	finish, err := service.AdmitSearch(ctx, "tavily", "key")
	if err != nil {
		b.Fatal(err)
	}
	finish(true)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(searchQuotaTTL + time.Second)
		finish, err := service.AdmitSearch(ctx, "tavily", "key")
		if err != nil {
			b.Fatal(err)
		}
		finish(true)
	}
	b.StopTimer()
	waitQuotaRefresh(service.routes["tavily"])
}

// BenchmarkAdmitSearchEnforcementOff measures a provider that has a historical
// search_quota row but is no longer enforced.
func BenchmarkAdmitSearchEnforcementOff(b *testing.B) {
	conf := config.Config{Exa: config.ExaConfig{APIKey: "key"}}
	conf.Dashboard.Quotas = config.QuotasConfig{
		Limits:           map[string]int{"exa": 1000000},
		EnforceProviders: []string{"exa"},
	}
	store := openBenchStore(b)
	defer store.Close()
	service := New(conf, store)
	if err := service.ResetProvider("exa"); err != nil {
		b.Fatal(err)
	}

	disabled := conf
	disabled.Dashboard.Quotas.EnforceProviders = nil
	service = New(disabled, store)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		finish, err := service.AdmitSearch(ctx, "exa", "key")
		if err != nil {
			b.Fatal(err)
		}
		finish(true)
	}
}

func waitQuotaRefresh(route *searchGuard) {
	if route == nil {
		return
	}
	for i := 0; i < 300; i++ {
		route.mu.Lock()
		pending := route.poll
		route.mu.Unlock()
		if pending == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
