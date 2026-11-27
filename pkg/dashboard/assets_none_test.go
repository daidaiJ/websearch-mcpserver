//go:build no_ui

package dashboard

import (
	"net/http"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func TestNoUIBuildWithoutExternalDirFailsClosed(t *testing.T) {
	resp := getDashboardPath(t, testDashboardMux(config.Config{}), dashboardPath)
	if resp.Code != http.StatusInternalServerError || !strings.Contains(resp.Body.String(), "no_ui") {
		t.Fatalf("no_ui build must explain the missing frontend: %d %s", resp.Code, resp.Body.String())
	}
}
