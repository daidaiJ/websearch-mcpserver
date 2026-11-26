package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"resty.dev/v3"

	"websearch/pkg/client"
	"websearch/pkg/config"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManualFetchUsesConfiguredRemoteFirstAndFallsBackOnce(t *testing.T) {
	var timeline []string
	origin := &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
		timeline = append(timeline, r.Method)
		return diagnosticResponse(r, http.StatusOK, "text/plain",
			"The library remains open all week. Visitors can check the catalog to find books and research material. Borrowing is available at the main desk."),
			nil
	})}
	prev := client.DefaultClient
	client.DefaultClient = resty.NewWithClient(&http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
		timeline = append(timeline, "tinyfish")
		var request struct { URLs []string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.URLs) != 1 {
			t.Error("provider did not receive exactly one target")
		}
		return diagnosticResponse(r, http.StatusTooManyRequests, "application/json", `{"error":"fixture"}`), nil
	})})
	t.Cleanup(func(){ client.DefaultClient = prev })
	conf := config.Config{}
	conf.MCPHub.Enabled = new(false)
	conf.MCPHub.FetchMode = config.MCPHubFetchPrefer
	conf.Keenable.Enabled = new(false)
	conf.TinyFish.APIKey = "fixture"
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	res, err := diagnoseWithPublicClient(ctx, conf, "https://93.184.216.34/page", origin)
	if err != nil || res.Node != "webfetch" || !res.RemoteRedirects || res.Characters < 100 ||
		!slices.Equal(timeline, []string{"HEAD", "tinyfish", "GET"}) ||
		!slices.Equal(res.Attempts, []string{"tinyfish", "webfetch"}) {
		t.Fatalf("trial=%+v, trace=%v, error=%v", res, timeline, err)
	}
}

func diagnosticResponse(r *http.Request, status int, mediaType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{"Content-Type": {mediaType}}, Body: io.NopCloser(strings.NewReader(body)),
		Request: r,
	}
}
