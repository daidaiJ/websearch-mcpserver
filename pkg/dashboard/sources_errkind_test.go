package dashboard

import (
	"testing"

	"websearch/pkg/telemetry"
)

func TestJoinSourcesMergesErrorKindsAcrossAliases(t *testing.T) {
	defs := []SourceView{
		{ID: "aliased", Configured: true, Active: true, aliases: []string{"new_alias", "old_alias"}},
	}
	observed := []telemetry.Health{
		{
			Name: "new_alias", Status: "healthy", SampleSize: 2,
			LastSeenAt: "2026-09-18T15:00:00+08:00",
			ErrorKinds: []telemetry.ErrorKindCount{{Kind: telemetry.ErrorKindNoResult, Count: 2}},
		},
		{
			Name: "old_alias", Status: "down", SampleSize: 6,
			LastSeenAt: "2026-09-18T14:00:00+08:00",
			ErrorKinds: []telemetry.ErrorKindCount{{Kind: telemetry.ErrorKindRateLimit, Count: 3}},
		},
	}
	out := joinSources(defs, observed)
	row := sourceByID(t, out, "aliased")
	counts := map[string]int{}
	for _, item := range row.ErrorKinds {
		counts[item.Kind] = item.Count
	}
	if counts[telemetry.ErrorKindRateLimit] != 3 || counts[telemetry.ErrorKindNoResult] != 2 {
		t.Fatalf("merged error kinds = %+v, want rate_limit=3 no_result=2", row.ErrorKinds)
	}
}
