package telemetry

import (
	"errors"
	"testing"
)

func TestClassifyErrorKind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ErrorKindNone},
		{"rate limit 429", errors.New("dblp HTTP 429"), ErrorKindRateLimit},
		{"rate limit text", errors.New("too many requests"), ErrorKindRateLimit},
		{"rate limit chinese", errors.New("请求过于频繁，请稍后再试"), ErrorKindRateLimit},
		{"captcha", errors.New("engine returned captcha challenge"), ErrorKindCaptcha},
		{"captcha chinese", errors.New("需要验证码"), ErrorKindCaptcha},
		{"parse html", errors.New("dblp parse: invalid character '<' looking for beginning of value"), ErrorKindParse},
		{"parse no text", errors.New("no text extracted from pdf"), ErrorKindParse},
		{"no result", errors.New("学术引擎搜索无结果"), ErrorKindNoResult},
		{"access denied", errors.New("crossref HTTP 403 forbidden"), ErrorKindAccessDenied},
		{"timeout", errors.New("context deadline exceeded"), ErrorKindTimeout},
		{"network", errors.New("dial tcp: connection refused"), ErrorKindNetwork},
		{"server error", errors.New("upstream returned status 502"), ErrorKindNetwork},
		{"unknown", errors.New("something odd happened"), ErrorKindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyErrorKind(tt.err); got != tt.want {
				t.Fatalf("ClassifyErrorKind(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
