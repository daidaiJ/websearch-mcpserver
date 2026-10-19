package testenv

import (
	"errors"
	"testing"
)

// TestClassifyScenarioErrors 场景类错误必须可归类，业务错误不误判为网络问题。
func TestClassifyScenarioErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"验证码", errors.New("duckduckgo: CAPTCHA challenge"), "blocked"},
		{"上游 429", errors.New("openalex HTTP 429"), "rate_limited"},
		{"限流文案", errors.New("rate limit exceeded"), "rate_limited"},
		{"网络超时", errors.New("dial tcp: i/o timeout"), "network"},
		{"业务错误", errors.New("parse failed: unexpected token"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.err)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("业务错误不应归类为场景类，实际 %q", got)
				}
				return
			}
			if len(got) < len(tc.want) || got[:len(tc.want)] != tc.want {
				t.Fatalf("classify(%v) = %q, 期望前缀 %q", tc.err, got, tc.want)
			}
		})
	}
}
