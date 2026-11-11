// Package errcode carries stable failure codes across package boundaries.
//
// Error text is localized and may change between releases; classification must
// not depend on it. Producers attach a code where the failure is detected, and
// consumers (diagnostics, telemetry, tool output) read it with errors.As before
// falling back to text matching.
package errcode

import "errors"

// Code is a stable failure category. Values are persisted in telemetry and
// exposed in diagnostics, so treat them as an API.
type Code string

const (
	Timeout      Code = "timeout"       // request timeout / context cancellation
	RateLimit    Code = "rate_limit"    // 429/202, local limiter, quota exhaustion
	Challenge    Code = "challenge"     // CAPTCHA / JS challenge / WAF
	Blocked      Code = "blocked"       // security policy block (SSRF, denied URL)
	AccessDenied Code = "access_denied" // 401/403 from an upstream API
	Parse        Code = "parse"         // response parse failure
	NoResult     Code = "no_result"     // valid response with zero results
	Network      Code = "network"       // DNS / connect / TLS / 5xx transport
	OffTopic     Code = "off_topic"     // result bucket does not match the query
	Unknown      Code = "unknown"       // classified, but no narrower category
)

// Error attaches a stable code to an underlying error without changing its
// message. It implements Unwrap so errors.Is/As keep working through it.
type Error struct {
	Code Code
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

// ErrorCode implements the coder interface read by CodeOf.
func (e *Error) ErrorCode() Code { return e.Code }

// New builds a coded error from a message.
func New(code Code, msg string) error {
	return &Error{Code: code, Err: errors.New(msg)}
}

// Wrap attaches code to err. A nil err stays nil.
func Wrap(code Code, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}

type messageError struct {
	msg string
	err error
}

func (e *messageError) Error() string { return e.msg }

func (e *messageError) Unwrap() error { return e.err }

// Message replaces the display text of err while preserving its chain, so
// errors.Is/As and CodeOf still see the original failures.
func Message(msg string, err error) error {
	if err == nil {
		return nil
	}
	return &messageError{msg: msg, err: err}
}

type coder interface {
	ErrorCode() Code
}

// CodeOf returns the nearest stable code attached to err's chain, or "" when
// none is present. Producers that already expose an ErrorCode method
// (including KeyError) are honored without importing this package.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var c coder
	if errors.As(err, &c) {
		return c.ErrorCode()
	}
	return ""
}
