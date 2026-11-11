package errcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeOf(t *testing.T) {
	coded := New(Challenge, "localized challenge text")
	if got := CodeOf(coded); got != Challenge {
		t.Fatalf("CodeOf = %q, want %q", got, Challenge)
	}
	if got := CodeOf(fmt.Errorf("outer: %w", coded)); got != Challenge {
		t.Fatalf("wrapped CodeOf = %q, want %q", got, Challenge)
	}
	if got := CodeOf(errors.New("plain")); got != "" {
		t.Fatalf("plain error code = %q, want empty", got)
	}
	if got := CodeOf(nil); got != "" {
		t.Fatalf("nil code = %q, want empty", got)
	}
	if err := Wrap(Timeout, nil); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
}

type externalCoded struct{ code Code }

func (e externalCoded) Error() string   { return "external" }
func (e externalCoded) ErrorCode() Code { return e.code }

func TestCodeOfExternalCoder(t *testing.T) {
	var err error = fmt.Errorf("wrapped: %w", externalCoded{code: RateLimit})
	if got := CodeOf(err); got != RateLimit {
		t.Fatalf("CodeOf = %q, want %q", got, RateLimit)
	}
}
