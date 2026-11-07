package ddg

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/search/core"
)

func TestDDGResponseFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		kind   core.FailureKind
	}{
		{"challenge-200", 200, `<form id="challenge-form"><div class="result">CAPTCHA</div></form>`, core.FailureChallenge},
		{"challenge-202", 202, `<form id='challenge-form'>CAPTCHA</form>`, core.FailureChallenge},
		{"http-error-with-results", 503, okHTML, core.FailureError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withDDGTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			e := cooldownTestEngine()
			resp, err := e.Search("query", 1, antirobot.TimeRangeNone)
			if err == nil || resp != nil {
				t.Fatalf("unexpected success: resp=%v err=%v", resp, err)
			}
			if got := core.ClassifyFailure(err); got != tc.kind {
				t.Fatalf("kind=%s, want %s: %v", got, tc.kind, err)
			}
		})
	}
}

func TestDDGLocalLimitIsFailure(t *testing.T) {
	e := cooldownTestEngine()
	e.limiter = antirobot.NewRateLimiter(1, 1)
	if !e.limiter.Allow() {
		t.Fatal("first request should be allowed")
	}
	resp, err := e.Search("query", 1, antirobot.TimeRangeNone)
	if err == nil || !strings.Contains(err.Error(), "local") || core.ClassifyFailure(err) != core.FailureRateLimit {
		t.Fatalf("local limit reported as success: resp=%v err=%v", resp, err)
	}
	if resp != nil && len(resp.Results) != 0 {
		t.Fatalf("本端限流应返回空结果, got %+v", resp)
	}
}

func TestDDGRetryPreservesChallenge(t *testing.T) {
	withShortCooldown(t, 20*time.Millisecond)
	var hits atomic.Int32
	withDDGTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		fmt.Fprint(w, `<form id="challenge-form">CAPTCHA</form>`)
	})
	e := cooldownTestEngine()
	_, err := e.Search("query", 1, antirobot.TimeRangeNone)
	if err == nil || core.ClassifyFailure(err) != core.FailureChallenge || hits.Load() != 2 {
		t.Fatalf("retry challenge lost: hits=%d err=%v", hits.Load(), err)
	}
}
