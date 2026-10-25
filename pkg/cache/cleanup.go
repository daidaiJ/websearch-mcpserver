package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
	"websearch/pkg/log"
)

// StaleMaxAge 缓存记录最大未命中存活时间（6小时）
const StaleMaxAge = 6 * time.Hour

// CleanupScheduler 后台定时清理协程
type CleanupScheduler struct {
	cache    *Cache
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	started  atomic.Bool
}

// NewCleanupScheduler 创建清理调度器
func NewCleanupScheduler(cache *Cache, interval time.Duration) *CleanupScheduler {
	return &CleanupScheduler{
		cache:    cache,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start 启动后台清理协程
func (s *CleanupScheduler) Start() {
	s.started.Store(true)
	go func() {
		defer close(s.done)
		log.InfoT("log.cache.cache_cleanup_goroutine_started_interval_eviction", "interval", s.interval, "staleMaxAge", StaleMaxAge)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				log.InfoT("log.cache.cache_cleanup_goroutine_stopped")
				return
			case <-ticker.C:
				_, err := s.cache.EvictStale(StaleMaxAge)
				if err != nil {
					log.ErrorT("log.cache.cache_cleanup_failed", "err", err)
				}
			}
		}
	}()
}

// Stop 停止清理协程并阻塞等待协程退出；ctx 取消时提前返回（停止信号仍已发出）。
// 未 Start 时仅发出停止信号并立即返回。
func (s *CleanupScheduler) Stop(ctx context.Context) {
	s.stopOnce.Do(func() { close(s.stop) })
	if !s.started.Load() {
		return
	}
	select {
	case <-s.done:
	case <-ctx.Done():
	}
}
