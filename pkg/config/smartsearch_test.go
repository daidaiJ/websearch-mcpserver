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
