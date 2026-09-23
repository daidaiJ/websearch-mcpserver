package quota

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"websearch/pkg/config"
)

func tavilyConf(key string) config.Config {
	return config.Config{Tavily: config.TavilyConfig{APIKey: key}}
}

// testService 用本地 httptest 服务替换官方端点，测试不访问真实网络。
func testService(t *testing.T, conf config.Config, handler http.Handler, client *http.Client) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	if client == nil {
		client = srv.Client()
	}
	return newService(conf, srv.URL+"/usage", client, nil)
}

func f64(v *float64) string {
	if v == nil {
		return "nil"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func TestGetTavilyAvailable(t *testing.T) {
	var gotAuth, gotPath string
	svc := testService(t, tavilyConf("tv-test-key"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"key":     map[string]any{"usage": 12.0, "limit": 100.0, "search_usage": 10.0, "extract_usage": 2.0},
			"account": map[string]any{"current_plan": "Researcher", "plan_usage": 323.0, "plan_limit": 1000.0},
		})
	}), nil)

	items := svc.Get(context.Background())
	if len(items) != 1 || items[0].Provider != "tavily" {
		t.Fatalf("items = %+v, want exactly one tavily entry", items)
	}
	item := items[0]
	if !item.Configured || item.Status != "available" || item.Unit != "credits" || item.Plan != "Researcher" || item.Error != "" {
		t.Fatalf("item = %+v", item)
	}
	if used, limit, remaining := f64(item.Used), f64(item.Limit), f64(item.Remaining); used != "323" || limit != "1000" || remaining != "677" {
		t.Fatalf("used/limit/remaining = %s/%s/%s, want 323/1000/677", used, limit, remaining)
	}
	if item.UpdatedAt == "" || item.Source != svc.endpoint {
		t.Fatalf("item = %+v", item)
	}
	if gotAuth != "Bearer tv-test-key" || gotPath != "/usage" {
		t.Fatalf("request auth=%q path=%q", gotAuth, gotPath)
	}
}

func TestGetReturnsOnlyTavilyWhenOtherProvidersConfigured(t *testing.T) {
	conf := config.Config{
		Tavily:    config.TavilyConfig{APIKey: "tv-test-key"},
		Anysearch: config.AnysearchConfig{APIKey: "as-key"},
		Exa:       config.ExaConfig{APIKey: "exa-key"},
		Doubao:    config.DoubaoConfig{APIKey: "db-key"},
		Jina:      config.JinaConfig{APIKey: "jina-key"},
	}
	svc := testService(t, conf, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"key":{"usage":1,"limit":10},"account":{}}`)
	}), nil)

	items := svc.Get(context.Background())
	if len(items) != 1 || items[0].Provider != "tavily" {
		t.Fatalf("items = %+v, want only tavily", items)
	}
}

func TestGetTavilyNotConfiguredSkipsNetwork(t *testing.T) {
	var hits int32
	svc := testService(t, config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}), nil)

	items := svc.Get(context.Background())
	if len(items) != 1 {
		t.Fatalf("items = %+v, want exactly one entry", items)
	}
	if item := items[0]; item.Provider != "tavily" || item.Configured || item.Status != "not_configured" {
		t.Fatalf("item = %+v, want tavily/not_configured", item)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("official endpoint hit %d times without a configured key", n)
	}
}

func TestGetTavilyFailures(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"detail":"unauthorized"}`, "401"},
		{"invalid json", http.StatusOK, `{"key":`, "无法解析"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const key = "tv-secret-key"
			svc := testService(t, tavilyConf(key), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}), nil)

			items := svc.Get(context.Background())
			if len(items) != 1 {
				t.Fatalf("items = %+v, want exactly one entry", items)
			}
			item := items[0]
			if item.Provider != "tavily" || !item.Configured || item.Status != "query_failed" {
				t.Fatalf("item = %+v", item)
			}
			if !strings.Contains(item.Error, tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", item.Error, tc.wantErr)
			}
			if strings.Contains(item.Error, key) {
				t.Fatalf("error leaks the API key: %q", item.Error)
			}
			if item.Used != nil || item.Limit != nil || item.Remaining != nil {
				t.Fatalf("failure must not carry numbers: %+v", item)
			}
		})
	}
}

func TestGetTavilyTimeout(t *testing.T) {
	svc := testService(t, tavilyConf("tv-test-key"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}), &http.Client{Timeout: 80 * time.Millisecond})

	start := time.Now()
	items := svc.Get(context.Background())
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("client timeout not honored, took %v", elapsed)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, want exactly one entry", items)
	}
	if item := items[0]; item.Status != "query_failed" || !strings.Contains(item.Error, "超时") {
		t.Fatalf("item = %+v, want query_failed timeout", item)
	}
}

func TestGetCachesTavilyForFiveMinutes(t *testing.T) {
	var hits int32
	svc := testService(t, tavilyConf("tv-test-key"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		io.WriteString(w, `{"key":{"usage":1,"limit":10},"account":{}}`)
	}), nil)
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }

	first := svc.Get(context.Background())
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("hits = %d, want 1", n)
	}
	now = now.Add(5*time.Minute - time.Second)
	second := svc.Get(context.Background())
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("hits = %d within cache TTL, want 1", n)
	}
	if second[0].UpdatedAt != first[0].UpdatedAt {
		t.Fatalf("cached entry changed: %q -> %q", first[0].UpdatedAt, second[0].UpdatedAt)
	}
	now = now.Add(2 * time.Second)
	svc.Get(context.Background())
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Fatalf("hits = %d after cache TTL, want 2", n)
	}
}

func TestTavilyItemPrefersAccountPlanTotals(t *testing.T) {
	var body tavilyUsageResponse
	body.Key.Usage = 0
	body.Key.Limit = 0
	body.Account.CurrentPlan = "Researcher"
	body.Account.PlanUsage = 323
	body.Account.PlanLimit = 1000

	item := tavilyItem(body, time.Unix(0, 0).UTC())
	if item.Used == nil || *item.Used != 323 {
		t.Fatalf("used = %v, want 323", item.Used)
	}
	if item.Limit == nil || *item.Limit != 1000 {
		t.Fatalf("limit = %v, want 1000", item.Limit)
	}
	if item.Remaining == nil || *item.Remaining != 677 {
		t.Fatalf("remaining = %v, want 677", item.Remaining)
	}
}

func TestTavilyItemFallsBackToKeyTotals(t *testing.T) {
	var body tavilyUsageResponse
	body.Key.Usage = 150
	body.Key.Limit = 1000

	item := tavilyItem(body, time.Unix(0, 0).UTC())
	if item.Used == nil || *item.Used != 150 {
		t.Fatalf("used = %v, want 150", item.Used)
	}
	if item.Limit == nil || *item.Limit != 1000 {
		t.Fatalf("limit = %v, want 1000", item.Limit)
	}
	if item.Remaining == nil || *item.Remaining != 850 {
		t.Fatalf("remaining = %v, want 850", item.Remaining)
	}
}
