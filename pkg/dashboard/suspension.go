package dashboard

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

var suspensionErrorKinds = map[string]string{
	"rate_limit":    telemetry.ErrorKindRateLimit,
	"captcha":       telemetry.ErrorKindCaptcha,
	"access_denied": telemetry.ErrorKindAccessDenied,
	"timeout":       telemetry.ErrorKindTimeout,
	"network":       telemetry.ErrorKindNetwork,
	"parse":         telemetry.ErrorKindParse,
	"no_result":     telemetry.ErrorKindNoResult,
}

// SuspensionPolicy exposes the parsed circuit breaker policy for the server
// bootstrap.
func SuspensionPolicy(conf config.SuspensionConfig) (telemetry.SuspensionPolicy, error) {
	return suspensionPolicyFromConfig(conf)
}

// suspensionPolicyFromConfig converts the YAML-facing duration strings into
// the telemetry policy. Invalid values are reported so the settings page can
// reject them before writing.
func suspensionPolicyFromConfig(conf config.SuspensionConfig) (telemetry.SuspensionPolicy, error) {
	policy := telemetry.SuspensionPolicy{}
	if strings.TrimSpace(conf.BanTimeOnFail) != "" {
		ms, err := parseSuspensionDuration(conf.BanTimeOnFail)
		if err != nil {
			return policy, fmt.Errorf("ban_time_on_fail: %w", err)
		}
		policy.BanTimeMS = ms
	}
	if strings.TrimSpace(conf.MaxBanTimeOnFail) != "" {
		ms, err := parseSuspensionDuration(conf.MaxBanTimeOnFail)
		if err != nil {
			return policy, fmt.Errorf("max_ban_time_on_fail: %w", err)
		}
		policy.MaxBanTimeMS = ms
	}
	if len(conf.SuspendedTimes) > 0 {
		policy.SuspendedTimesMS = make(map[string]int64, len(conf.SuspendedTimes))
		for rawKind, rawValue := range conf.SuspendedTimes {
			kind, ok := suspensionErrorKinds[strings.TrimSpace(rawKind)]
			if !ok {
				return policy, fmt.Errorf("suspended_times: unknown error kind %q", rawKind)
			}
			ms, err := parseSuspensionDuration(rawValue)
			if err != nil {
				return policy, fmt.Errorf("suspended_times.%s: %w", rawKind, err)
			}
			policy.SuspendedTimesMS[kind] = ms
		}
	}
	return policy.Normalize(), nil
}

// parseSuspensionDuration accepts Go duration strings ("5s", "10m", "1h") and
// bare numbers, which are read as seconds for convenience.
func parseSuspensionDuration(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return int64(seconds * 1000), nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", raw)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return parsed.Milliseconds(), nil
}
