package provider

import (
	"errors"
	"testing"
	"time"
)

// ── KeyPool SK 去重 ──────────────────────────────────────────────────────

func TestKeyPool_Dedup(t *testing.T) {
	pool, err := NewKeyPool([]string{"k1", "k1", "k2", "k1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.Len() != 2 {
		t.Errorf("expected 2 keys after dedup, got %d", pool.Len())
	}
	if pool.Available() != 2 {
		t.Errorf("expected 2 available keys, got %d", pool.Available())
	}
}

func TestKeyPool_Dedup_TrimsWhitespace(t *testing.T) {
	pool, err := NewKeyPool([]string{" k1 ", "k1", "\tk2\t"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.Len() != 2 {
		t.Errorf("expected 2 keys after trim+dedup, got %d", pool.Len())
	}
	// key 值应已 trim
	if got := pool.Next(); got != "k1" {
		t.Errorf("expected trimmed key \"k1\", got %q", got)
	}
}

func TestKeyPool_Dedup_AllSame(t *testing.T) {
	pool, err := NewKeyPool([]string{"k1", "k1", "k1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.Len() != 1 {
		t.Errorf("expected 1 key after dedup, got %d", pool.Len())
	}
}

func TestKeyPool_Dedup_MarkInvalid_AffectsSingleKey(t *testing.T) {
	pool, _ := NewKeyPool([]string{"k1", "k1"})
	pool.MarkInvalid("k1")
	if pool.Available() != 0 {
		t.Errorf("duplicate keys should collapse to one; expected 0 available after invalidating, got %d", pool.Available())
	}
}

func TestKeyPool_EmptyStringsFiltered(t *testing.T) {
	pool, err := NewKeyPool([]string{"", "  ", "k1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.Len() != 1 {
		t.Errorf("expected 1 key, got %d", pool.Len())
	}
}

func TestKeyPool_AllEmpty_Error(t *testing.T) {
	_, err := NewKeyPool([]string{"", "  "})
	if err == nil {
		t.Fatal("expected error for all-empty keys")
	}
}

// TestKeyPool_MarkInvalidFor_KeepsLongerUntil 并发短冷却不得缩短尚未到期的长冷却。
// 旧实现无条件覆写 invalidUntil：auth 30min 之后的 429 5min 会把 key 提前放行。
func TestKeyPool_MarkInvalidFor_KeepsLongerUntil(t *testing.T) {
	pool, err := NewKeyPool([]string{"k1"})
	if err != nil {
		t.Fatal(err)
	}
	pool.MarkInvalidFor("k1", 30*time.Minute)
	pool.mu.Lock()
	first := pool.states[0].invalidUntil
	pool.mu.Unlock()

	pool.MarkInvalidFor("k1", 5*time.Minute)
	pool.mu.Lock()
	second := pool.states[0].invalidUntil
	pool.mu.Unlock()

	if second.Before(first) {
		t.Fatalf("短 TTL 覆盖了更晚的截止: %v → %v", first, second)
	}
	if pool.Available() != 0 {
		t.Fatal("长冷却期间 key 必须仍不可用")
	}
}

func TestNewKeyError_ZeroKindIsTransport(t *testing.T) {
	ke := NewKeyError("", "k", errors.New("x"))
	if ke.Kind != KeyErrorTransport {
		t.Fatalf("零 Kind 应强制写成 transport, got %q", ke.Kind)
	}
}
