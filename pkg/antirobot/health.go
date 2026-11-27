package antirobot

import (
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"websearch/pkg/log"
)

// 引擎冷却状态统一注册表与落盘持久化（跨进程熔断复用）。
//
// 设计参考 free-search-mcp engine_health.json：冷却截止时间以墙钟落盘，
// 新进程启动时收养未过期冷却，避免频繁冷启动反复撞死引擎；已过期条目
// 保留 24h 记忆窗，使"连续限流翻倍升级"跨进程续接。文件是 advisory 的——
// 损坏或缺失只损失一次试探，不影响服务可用性。
//
// 引擎维护退避档位，并使用注册表给出的路由冷却对象。手动解除只放行
// 当前冷却，不改变各引擎的本地限速器或退避档位。
// 失败类型口径与 core.FailureKind 对齐（rate_limit / challenge…），antirobot
// 为底层包不反向依赖 core，故以字符串常量表达。

// 失败类型口径（对齐 core.FailureKind，本包不反向依赖 core）。
const (
	HealthKindRateLimit = "rate_limit"
	HealthKindChallenge = "challenge"
)

// healthRememberExpired 过期条目的记忆窗：冷却结束后仍保留该时长，
// 引擎重启收养时仅续接翻倍档位（不处于冷却期）。
const healthRememberExpired = 24 * time.Hour

// HealthEntry 单引擎冷却状态（落盘与观测共用）。
type HealthEntry struct {
	Engine   string  `json:"engine"`
	Kind     string  `json:"kind"`       // 失败类型（HealthKind*，口径对齐 core.FailureKind）
	Reason   string  `json:"reason"`     // 短原因（上游状态摘要，不含请求细节）
	Until    float64 `json:"until"`      // 冷却截止（Unix 秒，墙钟）
	Cooldown float64 `json:"cooldown"`   // 本次冷却时长（秒），下次失败翻倍的起点
	Updated  float64 `json:"updated_at"` // 最近更新时间（Unix 秒）
}

var (
	healthMu      sync.Mutex
	healthStates  = make(map[string]HealthEntry) // 本进程内的冷却状态（按引擎名）
	routeReleases = make(map[string]*atomic.Bool)
	healthPath    string // 落盘路径；空 = 仅内存（单测默认，无文件副作用）
)

// RouteCooldown is the registered deadline an engine checks before routing.
type RouteCooldown struct {
	until    time.Time
	released *atomic.Bool
}

func (c RouteCooldown) Remaining() time.Duration {
	if c.released == nil || c.released.Load() {
		return 0
	}
	return max(0, time.Until(c.until))
}

func routeCooldownLocked(engine string, until time.Time) RouteCooldown {
	released := routeReleases[engine]
	if released == nil {
		released = &atomic.Bool{}
		routeReleases[engine] = released
	}
	return RouteCooldown{until: until, released: released}
}

// SetHealthStore 设置冷却落盘路径（进程启动时调用一次）。path 为空时关闭落盘
// 并清空运行状态（单测隔离用）。
func SetHealthStore(path string) {
	healthMu.Lock()
	defer healthMu.Unlock()
	for _, released := range routeReleases {
		released.Store(true)
	}
	healthPath = path
	healthStates = make(map[string]HealthEntry)
	routeReleases = make(map[string]*atomic.Bool)
	if path != "" {
		if rows, err := readHealthFile(path); err == nil && rows != nil {
			healthStates = rows
		}
	}
}

// EnterCooldown 登记引擎的路由冷却并落盘，让新进程继承。
func EnterCooldown(engine, kind, reason string, until time.Time, cooldown time.Duration) RouteCooldown {
	if engine == "" {
		return RouteCooldown{}
	}
	healthMu.Lock()
	defer healthMu.Unlock()
	healthStates[engine] = HealthEntry{
		Engine:   engine,
		Kind:     kind,
		Reason:   reason,
		Until:    float64(until.UnixMilli()) / 1000,
		Cooldown: cooldown.Seconds(),
		Updated:  float64(time.Now().UnixMilli()) / 1000,
	}
	persistHealthLocked()
	return routeCooldownLocked(engine, until)
}

// ClearCooldown 引擎搜索成功后清除冷却记录（recordSuccess 时调用）。
func ClearCooldown(engine string) {
	healthMu.Lock()
	defer healthMu.Unlock()
	if _, had := healthStates[engine]; had {
		delete(healthStates, engine)
		persistHealthLocked()
	}
}

// ReleaseRouteCooldown releases an active source, or all active sources for
// an empty name. Holding the registry lock serializes release and entry.
func ReleaseRouteCooldown(engine string) int {
	healthMu.Lock()
	defer healthMu.Unlock()
	now := float64(time.Now().UnixMilli()) / 1000
	count := 0
	for name, row := range healthStates {
		if row.Until <= now || (engine != "" && name != engine) {
			continue
		}
		if released := routeReleases[name]; released != nil {
			released.Store(true)
			delete(routeReleases, name)
		}
		delete(healthStates, name)
		count++
	}
	if count > 0 {
		persistHealthLocked()
	}
	return count
}

// AdoptCooldown reports the persisted deadline and escalation level. An expired
// deadline within the memory window still supplies the last escalation level.
func AdoptCooldown(engine string) (time.Time, time.Duration, bool) {
	healthMu.Lock()
	defer healthMu.Unlock()
	return adoptedCooldownLocked(engine)
}

// AdoptRouteCooldown restores the persisted route gate so it can be released
// together with cooldowns entered during the current process.
func AdoptRouteCooldown(engine string) (RouteCooldown, time.Duration) {
	healthMu.Lock()
	defer healthMu.Unlock()
	until, cooldown, ok := adoptedCooldownLocked(engine)
	if !ok || until.IsZero() {
		return RouteCooldown{}, cooldown
	}
	return routeCooldownLocked(engine, until), cooldown
}

func adoptedCooldownLocked(engine string) (time.Time, time.Duration, bool) {
	row, present := healthStates[engine]
	if healthPath == "" || !present {
		return time.Time{}, 0, false
	}
	until := time.UnixMilli(int64(row.Until * 1000))
	remaining := time.Until(until)
	cooldown := time.Duration(row.Cooldown * float64(time.Second))
	if remaining > 0 {
		return until, cooldown, true
	}
	if remaining > -healthRememberExpired {
		return time.Time{}, cooldown, true
	}
	return time.Time{}, 0, false
}

// HealthSnapshot 当前未过期冷却快照（供 MCP Resource 观测，不含任何密钥）。
// 返回按引擎名排序的副本；过期条目惰性剔除。
func HealthSnapshot() []HealthEntry {
	healthMu.Lock()
	defer healthMu.Unlock()
	now := float64(time.Now().UnixMilli()) / 1000
	out := make([]HealthEntry, 0, len(healthStates))
	for _, e := range healthStates {
		if e.Until <= now {
			continue
		}
		out = append(out, e)
	}
	for i := 1; i < len(out); i++ { // 少量元素，插入排序即可
		for j := i; j > 0 && out[j].Engine < out[j-1].Engine; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// HealthPersistEnabled 冷却落盘是否启用（health 资源展示用）。
func HealthPersistEnabled() bool {
	healthMu.Lock()
	defer healthMu.Unlock()
	return healthPath != ""
}

// readHealthFile 读取落盘文件为引擎名 → 条目映射。
func readHealthFile(path string) (map[string]HealthEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows map[string]HealthEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// persistHealthLocked 把未过期与记忆窗内的冷却条目落盘。
// advisory 文件：失败只打日志，绝不影响搜索主流程。
func persistHealthLocked() {
	path := healthPath
	if path == "" {
		return
	}
	rows := make(map[string]HealthEntry, len(healthStates))
	now := float64(time.Now().UnixMilli()) / 1000
	for name, e := range healthStates {
		// 冷却结束超过记忆窗的条目不再落盘（翻倍升级记忆随之作废，回到初始档位）
		if e.Until+healthRememberExpired.Seconds() < now {
			continue
		}
		rows[name] = e
	}

	data, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		log.WarnT("log.antirobot.engine_health_serialization_failed", "err", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.WarnT("log.antirobot.engine_health_write_failed", "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.WarnT("log.antirobot.engine_health_persist_failed", "err", err)
		_ = os.Remove(tmp)
	}
}
