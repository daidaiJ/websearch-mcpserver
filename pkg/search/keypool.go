package search

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websearch/pkg/log"
)

const keyInvalidTTL = 30 * time.Minute // Key 失效后冷却时间

// keyState 单个 key 的状态。
type keyState struct {
	invalidUntil time.Time // 失效截止时间，零值表示有效
}

// KeyPool 线程安全的 API Key 轮询池，支持失效标记与自动恢复。
type KeyPool struct {
	keys    []string
	states  []keyState
	idx     atomic.Uint64
	lastKey atomic.Value // 最近一次 Next() 返回的 key（string）
	mu      sync.Mutex   // 保护 states
}

// NewKeyPool 创建 KeyPool，keys 必须非空。
// 同一池内重复 Key 自动去重（trim 后比对，保留首次出现顺序），
// 避免用户配置了相同的 Key 被当成多个可用 Key 轮询。
func NewKeyPool(keys []string) (*KeyPool, error) {
	filtered := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	dup := 0
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			dup++
			continue
		}
		seen[k] = struct{}{}
		filtered = append(filtered, k)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("keypool: 无有效 API Key")
	}
	if dup > 0 {
		log.Infof("keypool: 检测到 %d 个重复 Key，已去重（剩余 %d 个）", dup, len(filtered))
	}
	return &KeyPool{
		keys:   filtered,
		states: make([]keyState, len(filtered)),
	}, nil
}

// Next 返回下一个可用 key（round-robin，跳过失效 key）。
// 所有 key 均失效时返回最早将恢复的那个。
func (p *KeyPool) Next() string {
	n := uint64(len(p.keys))
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	// 尝试找到一个有效 key
	var earliestRecover time.Time
	for i := uint64(0); i < n; i++ {
		idx := (p.idx.Add(1) - 1) % n
		if p.states[idx].invalidUntil.IsZero() || now.After(p.states[idx].invalidUntil) {
			p.states[idx].invalidUntil = time.Time{}
			p.lastKey.Store(p.keys[idx])
			return p.keys[idx]
		}
		if earliestRecover.IsZero() || p.states[idx].invalidUntil.Before(earliestRecover) {
			earliestRecover = p.states[idx].invalidUntil
		}
	}

	// 全部失效，返回最早恢复的那个（不等待）
	for i := range p.states {
		if p.states[i].invalidUntil.Equal(earliestRecover) {
			p.lastKey.Store(p.keys[i])
			return p.keys[i]
		}
	}
	p.lastKey.Store(p.keys[0])
	return p.keys[0]
}

// MarkInvalid 标记指定 key 失效 30 分钟。
func (p *KeyPool) MarkInvalid(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, k := range p.keys {
		if k == key {
			p.states[i].invalidUntil = time.Now().Add(keyInvalidTTL)
			return
		}
	}
}

// MarkLastInvalid 标记 Next() 最近返回的 key 失效 30 分钟。
// 注意：非并发安全（lastKey 是池级状态，并发 Next 会标错 key），新代码勿用；
// 请改用 MarkInvalid(key) 配合 KeyError 精确标记本次实际使用的 key。
func (p *KeyPool) MarkLastInvalid() {
	v := p.lastKey.Load()
	if v == nil {
		return
	}
	p.MarkInvalid(v.(string))
}

// Len 返回池中 key 总数。
func (p *KeyPool) Len() int { return len(p.keys) }

// Available 返回当前有效 key 数量。
func (p *KeyPool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	count := 0
	for _, s := range p.states {
		if s.invalidUntil.IsZero() || now.After(s.invalidUntil) {
			count++
		}
	}
	return count
}
