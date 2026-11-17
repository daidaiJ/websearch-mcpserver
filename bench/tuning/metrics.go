package tuning

import (
	"math"
	"math/rand"
	"net/url"
	"sort"
	"strings"

	"websearch/pkg/search/core"
	"websearch/pkg/search/enhance"
)

// CaseMetrics is the per-query score reported by the benchmark.
type CaseMetrics struct {
	Precision float64 // P@K, fixed denominator K
	Recall    float64 // distinct expected domains found / expected domains
	MRR       float64 // 1 / rank of first matching result, 0 when absent
}

// Aggregate is the macro average over cases.
type Aggregate struct {
	Precision float64
	Recall    float64
	MRR       float64
}

// Evaluate runs the scoring pipeline over the dataset with the given options.
func Evaluate(ds Dataset, opts enhance.Options) []CaseMetrics {
	return EvaluateWithWeights(ds, opts, nil)
}

// EvaluateWithWeights additionally overrides per-engine weights. The map is
// keyed by the normalized engine name used in the dataset.
func EvaluateWithWeights(ds Dataset, opts enhance.Options, weights map[string]float64) []CaseMetrics {
	out := make([]CaseMetrics, 0, len(ds.Cases))
	for _, tc := range ds.Cases {
		buckets := make([]core.ScoreBucket, 0, len(tc.Engines))
		for _, e := range tc.Engines {
			w := e.Weight
			if override, ok := weights[e.Name]; ok {
				w = override
			}
			buckets = append(buckets, core.ScoreBucket{Name: e.Name, Weight: w, Results: e.Results})
		}
		got := enhance.EnhanceResultsWithOptions(tc.Query, buckets, opts)
		out = append(out, Score(got, tc.ExpectedDomains, ds.K))
	}
	return out
}

// Score computes P@K / R@K / MRR@K against expected domains. A result matches
// an expected domain when its host equals that domain or is a subdomain of it
// (one-way: expected python.org matches docs.python.org; expected
// docs.python.org does not match python.org).
func Score(results []core.SearchResult, expected []string, k int) CaseMetrics {
	if k <= 0 {
		k = 8
	}
	if len(results) > k {
		results = results[:k]
	}
	matchedDomains := make(map[string]struct{}, len(expected))
	hits := 0
	first := 0
	for i, r := range results {
		host := hostOf(r.Url)
		if host == "" {
			continue
		}
		for _, want := range expected {
			if hostMatches(host, want) {
				hits++
				matchedDomains[want] = struct{}{}
				if first == 0 {
					first = i + 1
				}
				break
			}
		}
	}
	m := CaseMetrics{Precision: float64(hits) / float64(k)}
	if len(expected) > 0 {
		m.Recall = float64(len(matchedDomains)) / float64(len(expected))
	}
	if first > 0 {
		m.MRR = 1 / float64(first)
	}
	return m
}

// Mean aggregates per-case metrics.
func Mean(metrics []CaseMetrics) Aggregate {
	var agg Aggregate
	if len(metrics) == 0 {
		return agg
	}
	for _, m := range metrics {
		agg.Precision += m.Precision
		agg.Recall += m.Recall
		agg.MRR += m.MRR
	}
	n := float64(len(metrics))
	agg.Precision /= n
	agg.Recall /= n
	agg.MRR /= n
	return agg
}

// DeltaStats is the paired comparison of a candidate configuration against the
// baseline. The confidence interval covers ΔMRR; P/R deltas are point estimates.
type DeltaStats struct {
	DeltaP, DeltaR, DeltaMRR float64
	LowMRR, HighMRR          float64
	Wins, Losses, Ties       int
	Samples                  int
}

// PairedBootstrap compares base and candidate by resampling query indices.
// The same indices are used for both sides, so query difficulty cancels out.
func PairedBootstrap(base, candidate []CaseMetrics, iterations int, seed int64) DeltaStats {
	if len(base) != len(candidate) || len(base) == 0 || iterations <= 0 {
		return DeltaStats{}
	}
	baseAgg, candAgg := Mean(base), Mean(candidate)
	stats := DeltaStats{
		DeltaP:   candAgg.Precision - baseAgg.Precision,
		DeltaR:   candAgg.Recall - baseAgg.Recall,
		DeltaMRR: candAgg.MRR - baseAgg.MRR,
		Samples:  len(base),
	}
	for i := range base {
		switch {
		case candidate[i].MRR > base[i].MRR+1e-9:
			stats.Wins++
		case candidate[i].MRR < base[i].MRR-1e-9:
			stats.Losses++
		default:
			stats.Ties++
		}
	}
	rng := rand.New(rand.NewSource(seed))
	deltas := make([]float64, iterations)
	for it := 0; it < iterations; it++ {
		var sum float64
		for i := 0; i < len(base); i++ {
			idx := rng.Intn(len(base))
			sum += candidate[idx].MRR - base[idx].MRR
		}
		deltas[it] = sum / float64(len(base))
	}
	sort.Float64s(deltas)
	stats.LowMRR = percentile(deltas, 0.025)
	stats.HighMRR = percentile(deltas, 0.975)
	return stats
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return strings.TrimPrefix(host, "www.")
}

func hostMatches(host, expected string) bool {
	expected = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(expected, "www."), "."))
	if host == "" || expected == "" {
		return false
	}
	return host == expected || strings.HasSuffix(host, "."+expected)
}
