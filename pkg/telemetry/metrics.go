package telemetry

import (
	"sort"
	"time"
)

// MetricsPoint is one source's passive activity for the current day.
type MetricsPoint struct {
	Kind          string
	Name          string
	Requests      int64
	Successes     int64
	Failures      int64
	CacheHits     int64
	DurationMS    int64
	DurationCount int64
	Buckets       []int64
}

// MetricsSnapshot groups today's tool and provider activity using the same
// day boundary as the dashboard.
type MetricsSnapshot struct {
	Day     string
	Buckets []float64
	Tools   []MetricsPoint
	Sources []MetricsPoint
}

// Metrics returns today's per-source aggregates for the metrics endpoint.
// It is purely derived from persisted events; nothing is probed.
func (s *Store) Metrics(buckets []float64) (MetricsSnapshot, error) {
	now := timeNow()
	day := now.In(s.location).Format("2006-01-02")
	start := time.Date(now.In(s.location).Year(), now.In(s.location).Month(), now.In(s.location).Day(), 0, 0, 0, 0, s.location).Unix()
	rows, err := s.db.Query(`SELECT kind,tool,provider,success,cache_hit,duration_ms
		FROM usage_events WHERE occurred_at>=? ORDER BY occurred_at`, start)
	if err != nil {
		return MetricsSnapshot{}, err
	}
	defer rows.Close()
	snapshot := MetricsSnapshot{Day: day, Buckets: buckets}
	tools := map[string]*MetricsPoint{}
	sources := map[string]*MetricsPoint{}
	for rows.Next() {
		var kind, tool, provider string
		var success, cacheHit int
		var durationMS int64
		if err := rows.Scan(&kind, &tool, &provider, &success, &cacheHit, &durationMS); err != nil {
			return MetricsSnapshot{}, err
		}
		target := tools
		name := tool
		if kind == "provider" {
			target, name = sources, provider
		}
		if name == "" {
			continue
		}
		point, ok := target[name]
		if !ok {
			point = &MetricsPoint{Kind: kind, Name: name, Buckets: make([]int64, len(buckets))}
			target[name] = point
		}
		point.Requests++
		if success == 1 {
			point.Successes++
		} else {
			point.Failures++
		}
		if cacheHit == 1 {
			point.CacheHits++
		}
		point.DurationMS += durationMS
		point.DurationCount++
		for i, bound := range buckets {
			if float64(durationMS) <= bound {
				point.Buckets[i]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return MetricsSnapshot{}, err
	}
	snapshot.Tools = sortedMetricsPoints(tools)
	snapshot.Sources = sortedMetricsPoints(sources)
	return snapshot, nil
}

func sortedMetricsPoints(values map[string]*MetricsPoint) []MetricsPoint {
	out := make([]MetricsPoint, 0, len(values))
	for _, point := range values {
		out = append(out, *point)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
