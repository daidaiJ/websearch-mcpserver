package bing

import (
	"sync"
	"time"
)

// RateLimiter 滑动窗口限流器。
type RateLimiter struct {
	mu        sync.Mutex
	perSec    int
	perMin    int
	secWindow []time.Time
	minWindow []time.Time
}

// NewRateLimiter 创建限流器。perSec=每秒上限，perMin=每分钟上限。
func NewRateLimiter(perSec, perMin int) *RateLimiter {
	return &RateLimiter{perSec: perSec, perMin: perMin}
}

// Allow 检查并记录一次请求，允许则返回 true。
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	r.secWindow = filterAfter(r.secWindow, now.Add(-time.Second))
	r.minWindow = filterAfter(r.minWindow, now.Add(-time.Minute))

	if len(r.secWindow) >= r.perSec || len(r.minWindow) >= r.perMin {
		return false
	}
	r.secWindow = append(r.secWindow, now)
	r.minWindow = append(r.minWindow, now)
	return true
}

func filterAfter(w []time.Time, cut time.Time) []time.Time {
	i := 0
	for _, t := range w {
		if t.After(cut) {
			w[i] = t
			i++
		}
	}
	return w[:i]
}
