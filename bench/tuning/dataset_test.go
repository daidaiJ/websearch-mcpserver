package tuning

import (
	"testing"

	"websearch/pkg/search/core"
)

func TestReferenceDatasetLoads(t *testing.T) {
	ds, err := ReferenceDataset()
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Cases) != 20 {
		t.Fatalf("cases = %d, want 20", len(ds.Cases))
	}
	if ds.K != 8 {
		t.Fatalf("K = %d, want 8", ds.K)
	}
	langs := map[string]int{}
	for _, tc := range ds.Cases {
		langs[tc.Language]++
		if tc.Query == "" || len(tc.ExpectedDomains) == 0 {
			t.Fatalf("%s: query or expected domains missing", tc.Name)
		}
		if len(tc.Engines) < 3 {
			t.Fatalf("%s: engines = %d, want at least 3", tc.Name, len(tc.Engines))
		}
		for _, e := range tc.Engines {
			if e.Name == "" || len(e.Results) == 0 {
				t.Fatalf("%s: empty engine %+v", tc.Name, e)
			}
		}
	}
	if langs["zh"] != 10 || langs["en"] != 10 {
		t.Fatalf("language split = %v, want 10 zh + 10 en", langs)
	}
}

func TestReferenceDatasetBaseline(t *testing.T) {
	ds, err := ReferenceDataset()
	if err != nil {
		t.Fatal(err)
	}
	metrics := Evaluate(ds, ProductionOptions())
	agg := Mean(metrics)
	t.Logf("baseline P@8=%.4f R@8=%.4f MRR@8=%.4f", agg.Precision, agg.Recall, agg.MRR)
	if agg.Precision < 0.20 || agg.Recall < 0.50 || agg.MRR < 0.50 {
		t.Fatalf("baseline regressed: %+v", agg)
	}

	again := Evaluate(ds, ProductionOptions())
	for i := range metrics {
		if metrics[i] != again[i] {
			t.Fatalf("non-deterministic replay at case %d: %+v vs %+v", i, metrics[i], again[i])
		}
	}
}

func TestScoreDomainMatching(t *testing.T) {
	results := []core.SearchResult{
		{Url: "https://docs.python.org/3/"},
		{Url: "https://www.python.org/"},
		{Url: "https://notpython.org/"},
	}
	got := Score(results, []string{"python.org"}, 8)
	if got.Precision != 2.0/8.0 || got.Recall != 1 || got.MRR != 1 {
		t.Fatalf("subdomain match = %+v", got)
	}
	// The expected side is not allowed to widen: docs.python.org does not
	// match a bare python.org result.
	got = Score(results, []string{"docs.python.org"}, 8)
	if got.Precision != 1.0/8.0 || got.MRR != 1 {
		t.Fatalf("expected domain must match only itself and deeper subdomains: %+v", got)
	}
}

func TestPairedBootstrapIdentical(t *testing.T) {
	metrics := Evaluate(mustDataset(t), ProductionOptions())
	stats := PairedBootstrap(metrics, metrics, 200, 7)
	if stats.DeltaMRR != 0 || stats.LowMRR != 0 || stats.HighMRR != 0 || stats.Wins != 0 || stats.Losses != 0 {
		t.Fatalf("identical configs should have zero delta: %+v", stats)
	}
}

func mustDataset(t *testing.T) Dataset {
	t.Helper()
	ds, err := ReferenceDataset()
	if err != nil {
		t.Fatal(err)
	}
	return ds
}
