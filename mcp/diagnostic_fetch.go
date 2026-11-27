package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/daidaiJ/go-webfetch/fetch"
	"github.com/daidaiJ/go-webfetch/guard"
	"github.com/daidaiJ/go-webfetch/parse"

	"websearch/pkg/admission"
	"websearch/pkg/config"
	"websearch/pkg/errcode"
	fetchcontent "websearch/pkg/fetch"
	"websearch/pkg/fetch/jina"
	"websearch/pkg/search/provider"
	"websearch/pkg/search/provider/mcphub"
)

const diagnosticBodyLimit = 2 << 20

type FetchDiagnostic struct {
	Node            string   `json:"node,omitempty"`
	Attempts        []string `json:"attempts"`
	BodyStatus      string   `json:"body_status"`
	Characters      int      `json:"characters"`
	RemoteRedirects bool     `json:"remote_redirects"`
}

type diagnosticFetcher func(context.Context, string) (string, error)

func (f diagnosticFetcher) Fetch(ctx context.Context, u string) (string, error) {
	return f(ctx, u)
}

// DiagnosticFetchServices describes the paid or quota-limited nodes that may
// run. This function only reads configuration and never contacts an upstream.
func DiagnosticFetchServices(conf config.Config) []string {
	var services []string
	if conf.MCPHub.GetFetchMode() != config.MCPHubFetchOff {
		if conf.MCPHub.IsEnabled() && slices.ContainsFunc(conf.MCPHub.GetServers(), func(s config.MCPHubServerConfig) bool {
			return s.IsEnabled() && mcphub.KnownServer(s.Name)
		}) {
			services = append(services, "mcphub")
		}
		if conf.APISourceEnabled("tinyfish") && len(conf.TinyFish.EffectiveSKList()) > 0 {
			services = append(services, "tinyfish")
		}
		if conf.APISourceEnabled("keenable") {
			services = append(services, "keenable")
		}
	}
	if conf.Jina.APIKey != "" {
		services = append(services, "jina")
	}
	return services
}

// DiagnoseFetchURL uses the configured cleanfetch order. Each node is called
// at most once. The public transport validates and pins the target at connect
// time and checks every redirect followed locally.
func DiagnoseFetchURL(ctx context.Context, conf config.Config, rawURL string) (FetchDiagnostic, error) {
	return diagnoseWithPublicClient(ctx, conf, rawURL, publicHTTPClient(0, &conf))
}

func diagnoseWithPublicClient(ctx context.Context, conf config.Config, rawURL string, client *http.Client) (FetchDiagnostic, error) {
	out := FetchDiagnostic{BodyStatus: "no_body", Attempts: []string{}}
	ctx = admission.WithDiagnostic(ctx)
	u, err := diagnosticPublicURL(ctx, rawURL)
	if err != nil {
		return out, err
	}
	release, err := admission.Acquire(ctx)
	if err != nil {
		return out, err
	}
	defer release()

	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirectHops {
			return errcode.Wrap(errcode.Blocked, errUnsafeRedirect)
		}
		if _, err := diagnosticPublicURL(req.Context(), req.URL.String()); err != nil {
			return errcode.Wrap(errcode.Blocked, errUnsafeRedirect)
		}
		return nil
	}
	// Probe redirects with the same pinned transport before any URL is sent to
	// a remote reader. That reader may resolve and follow redirects independently.
	preflight, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
	if err != nil {
		return out, errcode.Wrap(errcode.Blocked, err)
	}
	head, err := client.Do(preflight)
	if err != nil {
		return out, err
	}
	head.Body.Close()
	if head.ContentLength > diagnosticBodyLimit {
		return out, errcode.New(errcode.Blocked, "diagnostic response limit")
	}
	u = head.Request.URL

	label := ""
	note := func(name string, remote bool) {
		out.Attempts = append(out.Attempts, name)
		out.RemoteRedirects = out.RemoteRedirects || remote
		label = name
	}
	local := func(ctx context.Context, rawURL string) (string, error) {
		note("webfetch", false)
		text, err := diagnosticWebFetch(ctx, client, rawURL)
		if err == nil || !allowFetchFallback(err) || errcode.CodeOf(err) == errcode.Blocked {
			return text, err
		}
		if reader := jina.NewFromConfig(conf.Jina, conf.Proxy); reader != nil {
			note("jina", true)
			result, err := reader.FetchContext(ctx, rawURL)
			if err != nil {
				return "", err
			}
			return result.Content, nil
		}
		return "", err
	}

	var remote []mcpHubFetcher
	if !isPDFURL(rawURL) && !isPDFURL(u.String()) {
		for _, name := range DiagnosticFetchServices(conf) {
			switch name {
			case "mcphub":
				hub, err := mcphub.New(conf.MCPHub, conf.Proxy.ProxyResolver(), nil)
				if err == nil {
					remote = append(remote, diagnosticFetcher(func(ctx context.Context, rawURL string) (string, error) {
						text, node, err := hub.DiagnosticFetch(ctx, rawURL)
						if node != "" {
							note(node, true)
						}
						return text, err
					}))
				}
			case "tinyfish", "keenable":
				keys := conf.TinyFish.EffectiveSKList()
				if name == "keenable" {
					keys = conf.Keenable.EffectiveSKList()
				}
				var pool *provider.KeyPool
				if len(keys) > 0 {
					var err error
					pool, err = provider.NewKeyPool(keys)
					if err != nil {
						continue
					}
				}
				remote = append(remote, diagnosticFetcher(func(ctx context.Context, rawURL string) (string, error) {
					note(name, true)
					if name == "tinyfish" {
						return provider.NewTinyFishSearch(pool).Fetch(ctx, rawURL, 16000)
					}
					return provider.NewKeenableSearch(pool, conf.Keenable.IsLive(), 0).Fetch(ctx, rawURL, 16000)
				}))
			}
		}
	}

	text, err := fetchPageWithFallback(ctx, u.String(), conf.MCPHub.GetFetchMode(), remote, local)
	if err != nil {
		return out, err
	}
	out.Node, out.BodyStatus, out.Characters = label, "available", utf8.RuneCountInString(text)
	return out, nil
}

func diagnosticPublicURL(ctx context.Context, rawURL string) (*url.URL, error) {
	if err := guard.ValidateURL(rawURL, 2048); err != nil {
		return nil, errcode.Wrap(errcode.Blocked, err)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || isPrivateHostFast(strings.ToLower(u.Hostname())) {
		return nil, errcode.New(errcode.Blocked, "diagnostic URL rejected")
	}
	u.Fragment = ""
	if _, err := publicDownloadIPs(ctx, u.Hostname()); err != nil {
		return nil, errcode.Wrap(errcode.Blocked, err)
	}
	return u, nil
}

func diagnosticWebFetch(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", errcode.Wrap(errcode.Blocked, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return "", errcode.New(errcode.NoResult, "diagnostic page not found")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, diagnosticBodyLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > diagnosticBodyLimit {
		return "", errcode.New(errcode.Blocked, "diagnostic response limit")
	}
	fr := &fhttp.Response{StatusCode: resp.StatusCode, Header: fhttp.Header(resp.Header), Body: io.NopCloser(bytes.NewReader(data))}
	if flagged, _ := fetch.NewDetector().Detect(fr, fr.Body); flagged {
		return "", errcode.New(errcode.Challenge, "diagnostic challenge")
	}
	out, err := parse.ProcessResponse(ctx, fr, resp.Request.URL.String(), resp.Header.Get("Content-Type"),
		parse.Options{PDF: &parse.PDFOptions{MaxPages: 10, Heuristic: true}})
	if err != nil {
		return "", errcode.Wrap(errcode.Parse, err)
	}
	if !fetchcontent.HasPageBody(out.Markdown, out.Title) || fetchcontent.AssessContent(out.Markdown).LowQuality() {
		return "", errcode.Wrap(errcode.NoResult, errors.New("diagnostic empty body"))
	}
	return out.Markdown, nil
}
