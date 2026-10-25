package telemetry

import (
	"path/filepath"
	"testing"
)

func TestLegacyProjectMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Record(Event{Kind: "tool", Client: "codex", Tool: "probe", Success: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("ALTER TABLE usage_events DROP COLUMN project"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, _, err := s.AttributionUsageStats(7)
	if err != nil || len(rows) != 1 || rows[0].Project != "" || rows[0].Requests != 1 || rows[0].Client != "codex" {
		t.Fatalf("legacy migration: %+v %v", rows, err)
	}
}
