package antirobot

import (
	"encoding/json"
	"os"
	"sync"
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
// 分工：翻倍档位计算与冷却期快速失败仍由各引擎自持（本注册表不改变引擎
// 限流语义）；引擎在 enterCooldown / recordSuccess 时同步登记，构造时收养。
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
	healthMu     sync.Mutex
	healthStates = make(map[string]HealthEntry) // 本进程内的冷却状态（按引擎名）
	healthPath   string                         // 落盘路径；空 = 仅内存（单测默认，无文件副作用）
)

// SetHealthStore 设置冷却落盘路径（进程启动时调用一次）。path 为空时关闭落盘
// 并清空运行状态（单测隔离用）。落盘文件与缓存数据库同目录，无独立配置键。
func SetHealthStore(path string) {
	healthMu.Lock()
	defer healthMu.Unlock()
	healthPath = path
	if path == "" {
		healthStates = make(map[string]HealthEntry)
	}
}

// EnterCooldown 登记一次冷却（引擎进入冷却时调用）。登记即落盘，让新进程继承。
func EnterCooldown(engine, kind, reason string, until time.Time, cooldown time.Duration) {
	if engine == "" {
		return
	}
	healthMu.Lock()
	healthStates[engine] = HealthEntry{
		Engine:   engine,
		Kind:     kind,
		Reason:   reason,
		Until:    float64(until.UnixMilli()) / 1000,
		Cooldown: cooldown.Seconds(),
		Updated:  float64(time.Now().UnixMilli()) / 1000,
	}
	path := healthPath
	healthMu.Unlock()
	persistHealth(path)
}

// ClearCooldown 引擎搜索成功后清除冷却（recordSuccess 时调用）。
func ClearCooldown(engine string) {
	if engine == "" {
		return
	}
	healthMu.Lock()
	_, had := healthStates[engine]
	delete(healthStates, engine)
	path := healthPath
	healthMu.Unlock()
	if had {
		persistHealth(path)
	}
}

// AdoptCooldown 引擎构造时收养落盘冷却。只读文件、不读进程内状态：
// 进程内状态由引擎自持，收养仅面向"上一次进程留下的冷却"。
//   - 冷却未过期：返回（until, 档位），引擎应继续避让至 until；
//   - 已过期但在记忆窗内：返回（零 until, 档位），引擎不避让但下次失败
//     从该档位翻倍，升级记忆跨进程存活；
//   - 无记录 / 超过记忆窗 / 文件不可读：返回 false，一切从零开始。
func AdoptCooldown(engine string) (until time.Time, cooldown time.Duration, ok bool) {
	if engine == "" {
		return time.Time{}, 0, false
	}
	healthMu.Lock()
	path := healthPath
	healthMu.Unlock()
	if path == "" {
		return time.Time{}, 0, false
	}
	rows, err := readHealthFile(path)
	if err != nil {
		return time.Time{}, 0, false // advisory 文件：损坏/缺失只损失一次试探
	}
	row, present := rows[engine]
	if !present {
		return time.Time{}, 0, false
	}
	untilTime := time.UnixMilli(int64(row.Until * 1000))
	remaining := time.Until(untilTime)
	if remaining > 0 {
		return untilTime, time.Duration(row.Cooldown * float64(time.Second)), true
	}
	if remaining > -healthRememberExpired {
		// 冷却已过期：档位作为翻倍起点续接，但不处于冷却期
		return time.Time{}, time.Duration(row.Cooldown * float64(time.Second)), true
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

// persistHealth 把未过期与记忆窗内的冷却条目落盘（temp + rename，尽量原子）。
// advisory 文件：失败只打日志，绝不影响搜索主流程。
func persistHealth(path string) {
	if path == "" {
		return
	}
	healthMu.Lock()
	rows := make(map[string]HealthEntry, len(healthStates))
	now := float64(time.Now().UnixMilli()) / 1000
	for name, e := range healthStates {
		// 冷却结束超过记忆窗的条目不再落盘（翻倍升级记忆随之作废，回到初始档位）
		if e.Until+healthRememberExpired.Seconds() < now {
			continue
		}
		rows[name] = e
	}
	healthMu.Unlock()

	data, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		log.Warnf("engine health 序列化失败: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Warnf("engine health 写盘失败: %v", err)
		return
	}
	// Windows 下 rename 不能覆盖已存在文件，先移除旧文件（advisory 文件可接受间隙）
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		log.Warnf("engine health 落盘失败: %v", err)
		_ = os.Remove(tmp)
	}
}
