package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"websearch/pkg/client"
	"websearch/pkg/config"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"

	"resty.dev/v3"
)

func TestManualEndpointProbes(t *testing.T) {
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"totalResults":0,"results":[]}`))
	}))
	defer endpoint.Close()
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	conf.Everything.URL = endpoint.URL
	conf.Proxy.Enabled = true
	conf.Proxy.Endpoint = strings.Replace(endpoint.URL, "://", "://private:fixture-secret@", 1) + "?key=fixture-query"
	_, mux := newWriteTestHandler(t, conf)

	path := "/__admin/api/diagnostics/everything"
	if got := doWrite(t, mux, http.MethodGet, path, "", "", "").Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("GET got %d", got)
	}
	if got := doWrite(t, mux, http.MethodPost, path, "{}", "192.0.2.2:80", "pw").Code; got != http.StatusForbidden {
		t.Fatalf("remote POST got %d", got)
	}
	if calls.Load() != 0 {
		t.Fatal("passive or rejected request probed the endpoint")
	}
	for _, kind := range []string{"everything", "proxy"} {
		rec := doWrite(t, mux, http.MethodPost, "/__admin/api/diagnostics/"+kind, "{}", "", "")
		var result struct {
			OK       bool   `json:"ok"`
			Endpoint string `json:"endpoint"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || !result.OK || result.Endpoint != endpoint.URL {
			t.Fatalf("%s: status=%d result=%s", kind, rec.Code, rec.Body)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("got %d HTTP probes, want one Everything request and a TCP dial for proxy", calls.Load())
	}
}

func TestManualProbeReportsFailedProxyDial(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	conf.Proxy.Enabled = true
	conf.Proxy.Endpoint = "http://" + listener.Addr().String()
	_, mux := newWriteTestHandler(t, conf)
	rec := doWrite(t, mux, http.MethodPost, "/__admin/api/diagnostics/proxy", "{}", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"error_kind":"network"`) {
		t.Fatalf("got status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestManualSearchRequiresPaidConfirmationAndRunsOnce(t *testing.T) {
	var calls atomic.Int32
	previous := client.DefaultClient
	client.DefaultClient = resty.NewWithClient(&http.Client{Transport: diagnosticTestTransport(func(r *http.Request) (*http.Response, error) {
		switch calls.Add(1) {
		case 1:
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": {"/unexpected"}},
				Body:       io.NopCloser(strings.NewReader("upstream says fixture-secret")),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"results":[{"title":"fixture-secret — Test result","url":"https://example.invalid","content":"Test result body","score":0.9}]}`)),
			}, nil
		}
	})})
	t.Cleanup(func() { client.DefaultClient = previous })
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	conf.Tavily.APIKey = "fixture-secret"
	h, mux := newWriteTestHandler(t, conf)
	oldTelemetry := telemetry.Default()
	telemetry.SetDefault(h.store)
	t.Cleanup(func() { telemetry.SetDefault(oldTelemetry) })

	path := "/__admin/api/diagnostics/search"
	rejected := doWrite(t, mux, http.MethodPost, path, `{"source":"tavily","query":"fixture"}`, "", "")
	if rejected.Code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("unconfirmed search ran: %d, %d", rejected.Code, calls.Load())
	}
	run := func() *httptest.ResponseRecorder {
		return doWrite(t, mux, http.MethodPost, path, `{"source":"tavily","query":"fixture","confirm":true}`, "", "")
	}
	redirect := run()
	if redirect.Code != http.StatusOK || calls.Load() != 1 || strings.Contains(redirect.Body.String(), "fixture-secret") {
		t.Fatalf("redirect trial got %d and %d calls: %s", redirect.Code, calls.Load(), redirect.Body)
	}
	success := run()
	if success.Code != http.StatusOK || calls.Load() != 2 || strings.Contains(success.Body.String(), "fixture-secret") ||
		!strings.Contains(success.Body.String(), `"titles":["[secret] — Test result"]`) {
		t.Fatalf("success trial got %d and %d calls: %s", success.Code, calls.Load(), success.Body)
	}
	overview, err := h.store.Overview()
	if err != nil || len(overview.Providers) != 0 || len(overview.Tools) != 0 {
		t.Fatalf("diagnostic searches appeared in the usage database: %+v (%v)", overview, err)
	}
}

type diagnosticTestTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManualFetchRejectsUnsupportedTargetBeforeRemoteProviders(t *testing.T) {
	var calls atomic.Int32
	previous := client.DefaultClient
	client.DefaultClient = resty.NewWithClient(&http.Client{Transport: diagnosticTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected upstream request")
	})})
	t.Cleanup(func() { client.DefaultClient = previous })
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	conf.TinyFish.APIKey = "fixture-secret"
	conf.CleanFetch.Enabled = true
	conf.MCPHub.FetchMode = "prefer"
	_, mux := newWriteTestHandler(t, conf)
	rec := doWrite(t, mux, http.MethodPost, "/__admin/api/diagnostics/fetch", `{"url":"file:///fixture","confirm":true}`, "", "")
	if rec.Code != http.StatusOK || calls.Load() != 0 || !strings.Contains(rec.Body.String(), `"error_kind":"blocked"`) {
		t.Fatalf("got status=%d calls=%d body=%s", rec.Code, calls.Load(), rec.Body)
	}
}

func TestManualSearchSupportsSingleRequestAcademicSources(t *testing.T) {
	var calls atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = diagnosticTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if strings.HasSuffix(r.URL.Path, "/esearch.fcgi") {
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<eSearchResult><IdList><Id>42</Id><Id>not-an-id</Id></IdList></eSearchResult>`)),
			}, nil
		}
		if strings.HasSuffix(r.URL.Path, "/efetch.fcgi") {
			t.Error("manual PubMed trial fetched details")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"results":[{"id":"https://openalex.org/W1","title":"Fixture publication"}]}`)),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "pw"
	conf.Academic.Enabled = true
	_, mux := newWriteTestHandler(t, conf)
	path := "/__admin/api/diagnostics/search"
	rec := doWrite(t, mux, http.MethodPost, path, `{"source":"openalex","query":"fixture"}`, "", "")
	if rec.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(rec.Body.String(), "Fixture publication") {
		t.Fatalf("academic trial got status=%d calls=%d body=%s", rec.Code, calls.Load(), rec.Body)
	}
	pubmed := doWrite(t, mux, http.MethodPost, path, `{"source":"pubmed","query":"fixture"}`, "", "")
	body := pubmed.Body.String()
	if pubmed.Code != http.StatusOK || calls.Load() != 2 || !strings.Contains(body, `"result_count":1`) ||
		!strings.Contains(body, "PMID 42") || !strings.Contains(body, "https://pubmed.ncbi.nlm.nih.gov/42/") {
		t.Fatalf("PubMed trial got status=%d calls=%d body=%s", pubmed.Code, calls.Load(), body)
	}
}

func TestManualSearchSharesHardQuotaWithoutWritingSkipTelemetry(t *testing.T) {
	var calls atomic.Int32
	previousClient := client.DefaultClient
	client.DefaultClient = resty.NewWithClient(&http.Client{Transport: diagnosticTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"results":[{"title":"Fixture result","url":"https://example.invalid","text":"Fixture result body"}]}`)),
		}, nil
	})})
	t.Cleanup(func() { client.DefaultClient = previousClient })
	conf := config.Config{Mode: config.ModeExa, Exa: config.ExaConfig{APIKey: "fixture"}}
	conf.Dashboard.AdminPassword = "pw"
	conf.Dashboard.Quotas = config.QuotasConfig{Limits: map[string]int{"exa": 1}, EnforceProviders: []string{"exa"}}
	h, mux := newWriteTestHandler(t, conf)
	oldTelemetry := telemetry.Default()
	telemetry.SetDefault(h.store)
	t.Cleanup(func() { telemetry.SetDefault(oldTelemetry) })

	production, err := search.NewFromConfigWithQuota(conf, h.quota)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := production.Primary.(search.ContextSearcher).SearchRawContext(t.Context(), "fixture"); err != nil {
		t.Fatal(err)
	}
	path := "/__admin/api/diagnostics/search"
	rec := doWrite(t, mux, http.MethodPost, path, `{"source":"exa","query":"fixture","confirm":true}`, "", "")
	if rec.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(rec.Body.String(), `"error_kind":"quota_skip"`) {
		t.Fatalf("quota-exhausted diagnostic got %d, calls=%d, body=%s", rec.Code, calls.Load(), rec.Body)
	}
	events, err := h.store.RecentFiltered(telemetry.EventFilter{Kind: telemetry.QuotaSkipEvent, Limit: 10})
	if err != nil || len(events) != 0 {
		t.Fatalf("diagnostic quota skip wrote usage telemetry: %+v (%v)", events, err)
	}
}
