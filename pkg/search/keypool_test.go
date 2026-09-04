package search

import (
	"testing"
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
