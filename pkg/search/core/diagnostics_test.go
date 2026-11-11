package core

import (
	"errors"
	"fmt"
	"testing"

	"websearch/pkg/errcode"
)

func TestClassifyFailureUsesCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FailureKind
	}{
		{"localized challenge", errcode.New(errcode.Challenge, "被拦截了"), FailureChallenge},
		{"wrapped rate limit", fmt.Errorf("engine: %w", errcode.New(errcode.RateLimit, "稍后再试")), FailureRateLimit},
		{"blocked is a security block", errcode.New(errcode.Blocked, "私有地址"), FailureError},
		{"coded timeout", errcode.New(errcode.Timeout, "请求超时"), FailureTimeout},
		{"unknown code falls back to text", errcode.New(errcode.Unknown, "context deadline exceeded"), FailureTimeout},
		{"keyword fallback", errors.New("rate limit reached"), FailureRateLimit},
		{"nil", nil, FailureError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyFailure(tt.err); got != tt.want {
				t.Fatalf("ClassifyFailure(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
