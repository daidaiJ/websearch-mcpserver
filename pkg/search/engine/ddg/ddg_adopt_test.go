package ddg

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/antirobot"
)

// adoptTestEngine 经 NewDuckDuckGo 构造（含收养落盘冷却步骤），注入测试用 client。
func adoptTestEngine(t *testing.T) *ddgEngine {
	t.Helper()
	eng, ok := NewDuckDuckGo(DuckDuckGoOpts{Enabled: true}).(*ddgEngine)
	if !ok {
		t.Fatal("NewDuckDuckGo 应返回 *ddgEngine")
	}
	eng.client = http.DefaultClient
	eng.ua = ddgUAs[0]
	return eng
}

// TestDDGAdoptPersistedCooldown 落盘冷却跨进程收养（P1-5）：
// 上一进程进入冷却 → 新引擎构造后应继续避让，并续接翻倍档位。
func TestDDGAdoptPersistedCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine_health.json")
	antirobot.SetHealthStore(path)
	t.Cleanup(func() { antirobot.SetHealthStore("") })

	until := time.Now().Add(time.Minute)
	antirobot.EnterCooldown("duckduckgo", antirobot.HealthKindRateLimit, "HTTP 202/429 限流", until, 40*time.Second)

	e := adoptTestEngine(t)
	if left := e.cooldownRemaining(); left <= 0 || left > time.Minute {
		t.Fatalf("应继承落盘冷却并避让, remaining=%v", left)
	}
	if e.cooldown != 40*time.Second {
		t.Fatalf("应续接翻倍档位 40s, got %v", e.cooldown)
	}

	// 成功后清冷却（本地 + 落盘注册表）
	e.recordSuccess()
	if e.cooldownRemaining() != 0 {
		t.Fatalf("成功后冷却应清零, remaining=%v", e.cooldownRemaining())
	}
}

// TestDDGAdoptExpiredKeepsEscalation 冷却已过期但在 24h 记忆窗内：
// 不避让，但下次失败从收养档位继续翻倍（升级记忆跨进程存活）。
func TestDDGAdoptExpiredKeepsEscalation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine_health.json")
	antirobot.SetHealthStore(path)
	t.Cleanup(func() { antirobot.SetHealthStore("") })

	antirobot.EnterCooldown("duckduckgo", antirobot.HealthKindRateLimit, "HTTP 202/429 限流", time.Now().Add(-time.Hour), 80*time.Second)

	e := adoptTestEngine(t)
	if left := e.cooldownRemaining(); left != 0 {
		t.Fatalf("过期冷却不应避让, remaining=%v", left)
	}
	if e.cooldown != 80*time.Second {
		t.Fatalf("应续接收养档位 80s, got %v", e.cooldown)
	}
}
