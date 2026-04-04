package cache

import (
	"context"
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
}

// NewCleanupScheduler 创建清理调度器
func NewCleanupScheduler(cache *Cache, interval time.Duration) *CleanupScheduler {
	return &CleanupScheduler{
		cache:    cache,
		interval: interval,
		stop:     make(chan struct{}),
	}
}

// Start 启动后台清理协程
func (s *CleanupScheduler) Start() {
	go func() {
		log.Infof("缓存清理协程已启动，间隔: %v，淘汰阈值: %v", s.interval, StaleMaxAge)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				log.Info("缓存清理协程已停止")
				return
			case <-ticker.C:
				_, err := s.cache.EvictStale(StaleMaxAge)
				if err != nil {
					log.Errf("缓存清理执行失败: %v", err)
				}
			}
		}
	}()
}

// Stop 停止清理协程（阻塞等待协程退出）
func (s *CleanupScheduler) Stop(ctx context.Context) {
	close(s.stop)
}
