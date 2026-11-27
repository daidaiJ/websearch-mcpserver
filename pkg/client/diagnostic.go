package client

import (
	"context"
	"io"
	"net/http"

	"resty.dev/v3"

	"websearch/pkg/admission"
)

// DiagnosticHTTPClient prevents a diagnostic request from following redirects.
func DiagnosticHTTPClient(base *http.Client) *http.Client {
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if configured, ok := transport.(*http.Transport); ok {
		isolated := configured.Clone()
		isolated.DisableKeepAlives = true
		transport = isolated
	}
	return &http.Client{
		Transport: transport,
		Timeout:   base.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Request starts a provider request. Diagnostics retain the configured proxy,
// with no redirects or retries and a bounded response.
func Request(ctx context.Context) *resty.Request {
	c := DefaultClient
	if admission.IsDiagnostic(ctx) {
		c = resty.NewWithClient(DiagnosticHTTPClient(c.Client())).SetResponseBodyLimit(4 << 20)
	}
	return c.R().SetContext(ctx)
}

// Do is the engine request entry point for both production and diagnostic calls.
func Do(c *http.Client, r *http.Request) (*http.Response, error) {
	if !admission.IsDiagnostic(r.Context()) {
		return c.Do(r)
	}
	resp, err := DiagnosticHTTPClient(c).Do(r)
	if err == nil && resp.Body != nil {
		resp.Body = &limitedBody{Reader: io.LimitReader(resp.Body, 4<<20), Closer: resp.Body}
	}
	return resp, err
}

type limitedBody struct {
	io.Reader
	io.Closer
}
