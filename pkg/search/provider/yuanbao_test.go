package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

const yuanbaoOKResp = `{
  "Response": {
    "Pages": [
      "{\"title\":\"元宝结果\",\"url\":\"https://example.com/a\",\"date\":\"2026-10-20 08:00:00\",\"passage\":\"标准摘要\",\"content\":\"动态摘要\",\"site\":\"Example\",\"score\":0.92}",
      "{\"title\":\"\",\"url\":\"https://example.com/b\",\"date\":\"2026/10/21 09:00:00\",\"content\":\"只有动态摘要\",\"site\":\"Example2\",\"score\":0.5}"
    ],
    "Query": "test",
    "Version": "standard",
    "RequestId": "rid-1"
  }
}`

func newYuanbaoTestServer(t *testing.T, status int, body string) (*YuanbaoSearchImpl, <-chan yuanbaoSearchRequest, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	reqCh := make(chan yuanbaoSearchRequest, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer yuanbao-test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var req yuanbaoSearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		reqCh <- req
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	pool, err := NewKeyPool([]string{"yuanbao-test-key"})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewYuanbaoSearch(pool, YuanbaoOptions{
		NumResults: 15,
		Mode:       2,
		Site:       "zhihu.com",
		Industry:   "news",
		ExcludeDomains: []string{
			"blocked.example",
		},
		MonthlyLimit: -1,
	})
	engine.endpoint = srv.URL
	return engine, reqCh, &calls
}

func TestYuanbao_HappyPath(t *testing.T) {
	engine, reqCh, _ := newYuanbaoTestServer(t, http.StatusOK, yuanbaoOKResp)
	results, err := engine.SearchRawWithTimeRange("三星堆", 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := <-reqCh
	if req.Query != "三星堆" || req.Cnt != 20 || req.Mode != 2 || req.Site != "zhihu.com" || req.Industry != "news" || req.Freshness != "m3" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if len(results) != 2 || results[0].Engine != "yuanbao" || results[0].Score != 0.92 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if results[0].Title != "元宝结果" || results[0].Content != "标准摘要" || results[0].PublishDate != "2026-10-20 08:00:00" {
		t.Fatalf("first result mapping: %+v", results[0])
	}
	if results[1].Title != "Example2" || results[1].Content != "只有动态摘要" {
		t.Fatalf("second result fallback mapping: %+v", results[1])
	}
}

func TestYuanbao_DefaultRequestOmitsOptionalFields(t *testing.T) {
	engine, reqCh, _ := newYuanbaoTestServer(t, http.StatusOK, yuanbaoOKResp)
	engine.numResults = 0
	engine.mode = 0
	engine.site = ""
	engine.industry = ""
	if _, err := engine.SearchRaw("query"); err != nil {
		t.Fatal(err)
	}
	raw := <-reqCh
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Cnt", "Mode", "Site", "Industry", "Freshness"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("%s must be omitted at default, got %s", key, b)
		}
	}
}

func TestYuanbao_BlacklistFiltersResults(t *testing.T) {
	engine, _, _ := newYuanbaoTestServer(t, http.StatusOK, yuanbaoOKResp)
	engine.excludeDomains = []string{"example.com"}
	_, err := engine.SearchRaw("query")
	if err == nil {
		t.Fatal("all results were blacklisted; expected an empty-result error")
	}
}

func TestYuanbao_AuthErrorIsKeyError(t *testing.T) {
	engine, _, _ := newYuanbaoTestServer(t, http.StatusOK, `{"Response":{"Error":{"Code":"UnauthorizedOperation","Message":"bad key"}}}`)
	_, err := engine.SearchRaw("query")
	var ke *KeyError
	if !errors.As(err, &ke) {
		t.Fatalf("want KeyError, got %v", err)
	}
}

func TestYuanbao_MonthlyBudgetStopsBeforeRequestAndPersists(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "yuanbao-quota.json")
	engine, _, calls := newYuanbaoTestServer(t, http.StatusOK, yuanbaoOKResp)
	engine.budget = newYuanbaoBudget(2, statePath)

	for i := 0; i < 2; i++ {
		if _, err := engine.SearchRaw("query"); err != nil {
			t.Fatalf("call %d should be allowed: %v", i+1, err)
		}
	}
	if _, err := engine.SearchRaw("query"); err == nil {
		t.Fatal("third call must be blocked by the monthly budget")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2", got)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("budget state was not persisted: %v", err)
	}

	// A restart with the same state file must keep the spent count.
	restarted, _, restartedCalls := newYuanbaoTestServer(t, http.StatusOK, yuanbaoOKResp)
	restarted.budget = newYuanbaoBudget(2, statePath)
	if _, err := restarted.SearchRaw("query"); err == nil {
		t.Fatal("restarted process must still respect the persisted budget")
	}
	if got := restartedCalls.Load(); got != 0 {
		t.Fatalf("blocked call reached upstream %d times", got)
	}
}

func TestYuanbao_BudgetResetsInNewMonth(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "yuanbao-quota.json")
	data, _ := json.Marshal(yuanbaoBudgetState{Month: "2000-01", Used: 99})
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	engine, _, calls := newYuanbaoTestServer(t, http.StatusOK, yuanbaoOKResp)
	engine.budget = newYuanbaoBudget(1, statePath)
	if _, err := engine.SearchRaw("query"); err != nil {
		t.Fatalf("stale month count must not block the new month: %v", err)
	}
	if _, err := engine.SearchRaw("query"); err == nil {
		t.Fatal("limit of 1 must block the second call")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestLookbackDaysToYuanbaoFreshness(t *testing.T) {
	tests := map[int]string{
		0: "", 1: "d1", 7: "d7", 30: "d30", 31: "m2", 90: "m3", 180: "m6", 360: "m12", 730: "y2", 3000: "y5",
	}
	for days, want := range tests {
		if got := lookbackDaysToYuanbaoFreshness(days); got != want {
			t.Errorf("lookbackDaysToYuanbaoFreshness(%d) = %q, want %q", days, got, want)
		}
	}
}

func TestNormalizeYuanbaoOptions(t *testing.T) {
	if got := normalizeYuanbaoCount(0); got != 0 {
		t.Fatalf("count 0 = %d, want 0 (omitted)", got)
	}
	for in, want := range map[int]int{5: 10, 10: 10, 15: 20, 50: 50, 99: 50} {
		if got := normalizeYuanbaoCount(in); got != want {
			t.Errorf("normalizeYuanbaoCount(%d) = %d, want %d", in, got, want)
		}
	}
	if got := normalizeYuanbaoMode(9); got != 0 {
		t.Errorf("invalid mode = %d, want 0", got)
	}
}

// ── 集成测试（从 gitignore 的 config.test.yaml 加载 API Key） ──

// TestYuanbao_SearchRaw_Integration 每次运行只发起一次上游调用，
// 避免消耗每月 2000 次的免费额度。
func TestYuanbao_SearchRaw_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadYuanbaoAPIKey(t)
	engine := NewYuanbaoSearch(newTestKeyPool(t, apiKey), YuanbaoOptions{
		NumResults:   10,
		MonthlyLimit: 1,
		StatePath:    filepath.Join(t.TempDir(), "yuanbao-quota.json"),
	})
	results, err := engine.SearchRaw("Go 泛型 性能")
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	for i, r := range results {
		t.Logf("[yuanbao %d] %s - %s", i+1, r.Title, r.Url)
		if r.Title == "" || r.Url == "" {
			t.Errorf("empty title/url: %+v", r)
		}
		if r.Engine != "yuanbao" {
			t.Errorf("engine = %q, want yuanbao", r.Engine)
		}
	}
}
