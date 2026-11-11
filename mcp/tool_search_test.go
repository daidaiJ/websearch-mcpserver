package mcpserver

import (
	"errors"
	"testing"

	"websearch/pkg/errcode"
)

func TestIsAntiBotFailureUsesCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"coded challenge with no keywords", errcode.New(errcode.Challenge, "被拦截"), true},
		{"coded blocked", errcode.New(errcode.Blocked, "安全策略"), false},
		{"coded timeout", errcode.New(errcode.Timeout, "超时"), false},
		{"keyword fallback", errors.New("captcha required"), true},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAntiBotFailure(tt.err); got != tt.want {
				t.Fatalf("isAntiBotFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
