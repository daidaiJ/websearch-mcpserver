package academic

import (
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/antirobot"
)

// TestArxivAdoptPersistedCooldown 落盘冷却跨进程收养（P1-5，与 ddg 同构）。
func TestArxivAdoptPersistedCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine_health.json")
	antirobot.SetHealthStore(path)
	t.Cleanup(func() { antirobot.SetHealthStore("") })

	antirobot.EnterCooldown("arxiv", antirobot.HealthKindRateLimit, "HTTP 429/503 限流", time.Now().Add(2*time.Minute), 60*time.Second)

	eng := NewArxiv(antirobot.ArxivOpts{}, nil)
	e, ok := eng.(*arxivEngine)
	if !ok {
		t.Fatal("NewArxiv 应返回 *arxivEngine")
	}
	if left := e.cooldownRemaining(); left <= 0 || left > 2*time.Minute {
		t.Fatalf("应继承落盘冷却并避让, remaining=%v", left)
	}
	if e.cooldown != 60*time.Second {
		t.Fatalf("应续接翻倍档位 60s, got %v", e.cooldown)
	}

	e.recordSuccess()
	if e.cooldownRemaining() != 0 {
		t.Fatalf("成功后冷却应清零, remaining=%v", e.cooldownRemaining())
	}
}
