package config

import (
	"testing"
	"time"
)

// TestSmartSearchEngineTimeout 软超时零值取默认 10s，负值关闭，正值按秒生效。
func TestSmartSearchEngineTimeout(t *testing.T) {
	cases := []struct {
		sec  int
		want time.Duration
	}{
		{0, 10 * time.Second},
		{-1, 0},
		{3, 3 * time.Second},
		{60, time.Minute},
	}
	for _, tc := range cases {
		got := SmartSearchConfig{EngineTimeoutSec: tc.sec}.EngineTimeout()
		if got != tc.want {
			t.Errorf("EngineTimeoutSec=%d → %v, 期望 %v", tc.sec, got, tc.want)
		}
	}
}

// TestSmartSearchPrimarySourceBoostEnabled 一手源加分未配置时默认开启，显式 false 才关闭。
func TestSmartSearchPrimarySourceBoostEnabled(t *testing.T) {
	off := false
	on := true
	cases := []struct {
		name string
		cfg  SmartSearchConfig
		want bool
	}{
		{"unset defaults on", SmartSearchConfig{}, true},
		{"explicit off", SmartSearchConfig{PrimarySourceBoost: &off}, false},
		{"explicit on", SmartSearchConfig{PrimarySourceBoost: &on}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.PrimarySourceBoostEnabled(); got != tc.want {
			t.Errorf("%s: PrimarySourceBoostEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
