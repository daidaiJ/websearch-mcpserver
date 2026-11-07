package provider

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websearch/pkg/i18n"
	"websearch/pkg/log"
)

const keyInvalidTTL = 30 * time.Minute // Key 失效后冷却时间

// keyState 单个 key 的状态。
type keyState struct {
	invalidUntil time.Time // 失效截止时间，零值表示有效
}

// KeyPool 线程安全的 API Key 轮询池，支持失效标记与自动恢复。
type KeyPool struct {
	keys   []string
	states []keyState
	idx    atomic.Uint64
	mu     sync.Mutex // 保护 states
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
		return nil, errors.New(i18n.T("provider.keypool.no_valid_key"))
	}
	if dup > 0 {
		log.InfoT("log.search.provider.keypool_detected_duplicate_keys_deduplicated_remaining", "dup", dup, "len", len(filtered))
	}
	return &KeyPool{
		keys:   filtered,
		states: make([]keyState, len(filtered)),
	}, nil
}

// Next 返回下一个可用 key（round-robin，跳过失效 key）。
// 所有 key 均冷却中时返回 ""（B8：不下发已知失效的 key，由调用方回退/报错）。
func (p *KeyPool) Next() string {
	key, _ := p.NextUsable()
	return key
}

// NextUsable 语义同 Next，另返回是否取到 key。
// 命中已过期的冷却 key 时顺手清零失效时间（惰性恢复，无定时器）。
func (p *KeyPool) NextUsable() (string, bool) {
	n := uint64(len(p.keys))
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	for i := uint64(0); i < n; i++ {
		idx := (p.idx.Add(1) - 1) % n
		if p.states[idx].invalidUntil.IsZero() || now.After(p.states[idx].invalidUntil) {
			p.states[idx].invalidUntil = time.Time{}
			return p.keys[idx], true
		}
	}
	return "", false
}

// MarkInvalid 标记指定 key 失效 30 分钟。
func (p *KeyPool) MarkInvalid(key string) {
	p.MarkInvalidFor(key, keyInvalidTTL)
}

// KeyRateLimitCooldown KeyErrorRateLimit 类失败的短冷却时长。
// 导出供 apipool 查表使用，避免跨包硬编码。
const KeyRateLimitCooldown = 5 * time.Minute

// MarkInvalidFor 标记指定 key 失效 ttl 时长（rate_limit 类短冷却用）。
// ttl <= 0 视为立即恢复（清零失效时间）。ttl > 0 时取
// max(已有截止, now+ttl)，避免并发 429 的短冷却覆盖尚未到期的鉴权冷却（B6 同类）。
func (p *KeyPool) MarkInvalidFor(key string, ttl time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, k := range p.keys {
		if k == key {
			if ttl <= 0 {
				p.states[i].invalidUntil = time.Time{}
			} else {
				until := time.Now().Add(ttl)
				if p.states[i].invalidUntil.After(until) {
					until = p.states[i].invalidUntil
				}
				p.states[i].invalidUntil = until
			}
			return
		}
	}
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

// KeyErrorKind 区分 key 失效原因，决定 apipool 的冷却与重试策略（B3）。
// KeyError 同时表达「key 坏了」与「这次调用失败了」两种语义，必须由 Kind 显式分类。
type KeyErrorKind string

const (
	// KeyErrorAuth 鉴权失败（401/403/invalid api key 等）→ 该 key 确实不可用，冷却 30 分钟。
	KeyErrorAuth KeyErrorKind = "auth"
	// KeyErrorRateLimit 限流 / 配额耗尽（429 等）→ 该 key 只需短冷却（5 分钟）。
	KeyErrorRateLimit KeyErrorKind = "rate_limit"
	// KeyErrorTransport 传输层失败（超时 / DNS / 连接重置 / 5xx）→ 与 key 无关，不冷却。
	KeyErrorTransport KeyErrorKind = "transport"
	// KeyErrorContent 业务性失败（空结果 / 上游业务报错）→ 与 key 无关，不冷却。
	KeyErrorContent KeyErrorKind = "content"
)

// KeyError 携带请求失败时实际使用的 API Key 与失效原因分类，供 apipool 按类查表处置。
// 注意：Error() 不输出 Key 本身，避免 API Key 泄漏到日志。
type KeyError struct {
	Key  string
	Kind KeyErrorKind // 零值 "" 按 transport 处理（保守：不冷却任何 key）
	Err  error
}

// NewKeyError 构造带分类的 KeyError。kind 为零值时强制写成 transport，
// 与 KeyError.Kind 字段注释及 apipool 查表的保守口径一致。
func NewKeyError(kind KeyErrorKind, key string, err error) *KeyError {
	if kind == "" {
		kind = KeyErrorTransport
	}
	return &KeyError{Key: key, Kind: kind, Err: err}
}

// keyErrorKindByStatus 把 HTTP 状态码映射为 KeyErrorKind：
// 401/403 鉴权失败 → auth；429 限流 → rate_limit；5xx → transport；其余 → content。
func keyErrorKindByStatus(status int) KeyErrorKind {
	switch {
	case status == 401 || status == 403:
		return KeyErrorAuth
	case status == 429:
		return KeyErrorRateLimit
	case status >= 500:
		return KeyErrorTransport
	default:
		return KeyErrorContent
	}
}

func (e *KeyError) Error() string {
	if e.Err == nil {
		return "key error"
	}
	return e.Err.Error()
}

func (e *KeyError) Unwrap() error { return e.Err }

// ErrNoAvailableKey 池内无可用 key 的稳定哨兵（errors.Is 判定），自身文本不做本地化。
var ErrNoAvailableKey = errors.New("provider: no available key")

// NoKeyError 返回包裹 ErrNoAvailableKey 的本地化错误，供各 provider 在
// NextUsable 取不到 key 时统一返回，避免携带空 key 打上游。
func NoKeyError(name string) error {
	return fmt.Errorf("%s: %w", i18n.T("provider.keypool.no_available_key", "Name", name), ErrNoAvailableKey)
}
