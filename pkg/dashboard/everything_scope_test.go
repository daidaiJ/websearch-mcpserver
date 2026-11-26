package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"websearch/pkg/config"
)

func TestEverythingScopeIsPrivateAndReadOnly(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "fixture"
	conf.Everything = config.EverythingConfig{URL: "https://private:fixture-userinfo@example.invalid:9001/?token=fixture-query", Roots: []string{"D:/fixture-private-root"}}
	_, mux := newWriteTestHandler(t, conf)
	const path = "/__admin/api/everything/scope"
	for _, tc := range []struct {
		method, remote, pass string
		status               int
	}{
		{"GET", "127.0.0.1:1234", "", 403}, {"GET", "192.0.2.15:1234", "fixture", 403},
		{"POST", "127.0.0.1:1234", "fixture", 405}, {"GET", "127.0.0.1:1234", "fixture", 200},
	} {
		rec := doWrite(t, mux, tc.method, path, "", tc.remote, tc.pass)
		if rec.Code != tc.status {
			t.Fatalf("%s %s: want %d, got %d", tc.method, tc.remote, tc.status, rec.Code)
		}
		if rec.Code == http.StatusOK {
			var out struct {
				URL   string
				Roots []string
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.URL != "https://example.invalid:9001" || len(out.Roots) != 1 || out.Roots[0] != conf.Everything.Roots[0] {
				t.Fatal("incorrect authorized scope")
			}
			if strings.Contains(rec.Body.String(), "fixture-userinfo") || strings.Contains(rec.Body.String(), "fixture-query") {
				t.Fatal("response includes credentials")
			}
		} else if strings.Contains(rec.Body.String(), "fixture-private-root") {
			t.Fatal("exposed private root")
		}
	}
}
