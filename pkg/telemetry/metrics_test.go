package telemetry

import "testing"

// TestMetricsExcludesObservationKinds guard 等观测事件不应计入工具请求数。
func TestMetricsExcludesObservationKinds(t *testing.T) {
	s := openTestStore(t)
	if err := s.Record(Event{Kind: "tool", Tool: "smartsearch", Success: true, Duration: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(Event{Kind: "guard", Tool: "smartsearch", Success: true, Detail: "mode=shadow"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Metrics([]float64{50, 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) != 1 {
		t.Fatalf("应只有 1 个工具聚合项，实际 %+v", snapshot.Tools)
	}
	if snapshot.Tools[0].Requests != 1 {
		t.Errorf("guard 事件被计入工具请求数: %+v", snapshot.Tools[0])
	}
}
