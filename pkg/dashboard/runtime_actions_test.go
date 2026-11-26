package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/quota"
)

func TestRuntimeActionsReleaseOneThenAllRoutes(t *testing.T) {
	antirobot.SetHealthStore("")
	t.Cleanup(func() { antirobot.SetHealthStore("") })
	until := time.Now().Add(time.Minute)
	first := antirobot.EnterCooldown("duckduckgo", antirobot.HealthKindRateLimit, "202", until, time.Minute)
	second := antirobot.EnterCooldown("arxiv", antirobot.HealthKindRateLimit, "429", until, time.Minute)

	conf := config.Config{}
	conf.Dashboard.AdminPassword = "admin"
	_, mux := newWriteTestHandler(t, conf)
	const path = "/__admin/api/sources/cooldowns/release"
	post := func(body, remote, password string, want int) {
		t.Helper()
		res := doWrite(t, mux, http.MethodPost, path, body, remote, password)
		if res.Code != want {
			t.Fatalf("POST %s returned %d, want %d: %s", body, res.Code, want, res.Body)
		}
	}
	post(`{"engine":"all","confirm":true}`, "203.0.113.2:1", "admin", http.StatusForbidden)
	post(`{"engine":"all","confirm":false}`, "", "", http.StatusBadRequest)
	post(`{"engine":"","confirm":true}`, "", "admin", http.StatusBadRequest)
	post(`{"engine":"duckduckgo","confirm":true}`, "", "", http.StatusOK)
	if first.Remaining() != 0 || second.Remaining() <= 0 {
		t.Fatalf("single release left duckduckgo=%s, arxiv=%s", first.Remaining(), second.Remaining())
	}
	post(`{"engine":"all","confirm":true}`, "", "", http.StatusOK)
	if second.Remaining() != 0 || len(antirobot.HealthSnapshot()) != 0 {
		t.Fatal("all-release did not clear the remaining route and dashboard snapshot")
	}
}

func TestRuntimeActionsRefreshQuotaReturnsPanelData(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.AdminPassword = "admin"
	_, mux := newWriteTestHandler(t, conf)
	const path = "/__admin/api/quotas/refresh"
	for i := range 2 {
		res := doWrite(t, mux, http.MethodPost, path, `{"confirm":true}`, "", "admin")
		if res.Code != http.StatusOK {
			t.Fatalf("refresh returned %d: %s", res.Code, res.Body)
		}
		var got struct {
			Items     []quota.Item `json:"items"`
			Refreshed bool         `json:"refreshed"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Refreshed != (i == 0) || len(got.Items) != 2 {
			t.Fatalf("refresh %d returned %+v", i, got)
		}
	}
}
