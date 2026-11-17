package tuning

import (
	"fmt"
	"strings"

	"websearch/pkg/config"
	"websearch/pkg/search/enhance"
)

// ProductionOptions returns the shipped scoring defaults plus the benchmark's
// top-8 cutoff. MMR is enabled because smartsearch.mmr.enabled defaults to true
// in config.Load.
func ProductionOptions() enhance.Options {
	opts := enhance.DefaultOptions()
	opts.Threshold = 0.05
	opts.MaxSize = 8
	opts.MMR = config.MMRConfig{Enabled: true, Lambda: 0.7}
	return opts
}

// Row is one parameter value's result.
type Row struct {
	Param   string
	Label   string
	Value   float64
	Agg     Aggregate
	Delta   DeltaStats
	Weights map[string]float64
}

// Report is a full sweep.
type Report struct {
	Dataset    string
	Cases      int
	Baseline   Aggregate
	Rows       []Row
	Iterations int
	Seed       int64
}

type sweepSpec struct {
	param   string
	value   float64
	weights map[string]float64
	mutate  func(*enhance.Options, float64)
}

// Sweep runs one-at-a-time parameter sweeps against the dataset.
func Sweep(ds Dataset, base enhance.Options, iterations int, seed int64) Report {
	baseMetrics := Evaluate(ds, base)
	report := Report{
		Dataset:    ds.Name,
		Cases:      len(ds.Cases),
		Baseline:   Mean(baseMetrics),
		Iterations: iterations,
		Seed:       seed,
	}
	for _, spec := range sweepSpecs() {
		opts := base
		if spec.mutate != nil {
			spec.mutate(&opts, spec.value)
		}
		metrics := EvaluateWithWeights(ds, opts, spec.weights)
		report.Rows = append(report.Rows, Row{
			Param:   spec.param,
			Label:   fmt.Sprintf("%s=%g", spec.param, spec.value),
			Value:   spec.value,
			Agg:     Mean(metrics),
			Delta:   PairedBootstrap(baseMetrics, metrics, iterations, seed),
			Weights: spec.weights,
		})
	}
	return report
}

func sweepSpecs() []sweepSpec {
	var specs []sweepSpec
	add := func(param string, values []float64, mutate func(*enhance.Options, float64)) {
		for _, v := range values {
			specs = append(specs, sweepSpec{param: param, value: v, mutate: mutate})
		}
	}
	add("rrf_k", []float64{20, 40, 60, 80, 120}, func(o *enhance.Options, v float64) { o.RRFK = v })
	add("lexical_weight", []float64{0, 0.25, 0.5, 0.75, 1}, func(o *enhance.Options, v float64) { o.LexicalWeight = v })
	add("consensus_scale", []float64{0, 0.5, 1, 1.5, 2}, func(o *enhance.Options, v float64) { o.ConsensusScale = v })
	add("authority_scale", []float64{0, 0.5, 1, 1.5, 2}, func(o *enhance.Options, v float64) { o.AuthorityScale = v })
	add("recency_scale", []float64{0, 0.5, 1, 1.5}, func(o *enhance.Options, v float64) { o.RecencyScale = v })
	add("domain_quality_scale", []float64{0, 0.5, 1, 1.5}, func(o *enhance.Options, v float64) { o.DomainQualityScale = v })
	add("rare_terms_scale", []float64{0, 0.5, 1, 1.5}, func(o *enhance.Options, v float64) { o.RareTermsScale = v })
	add("threshold", []float64{0.02, 0.05, 0.1, 0.2}, func(o *enhance.Options, v float64) { o.Threshold = v })
	add("mmr_lambda", []float64{0.3, 0.5, 0.7, 0.9, 1}, func(o *enhance.Options, v float64) {
		o.MMR = config.MMRConfig{Enabled: true, Lambda: v}
	})
	for _, v := range []float64{0, 1} {
		specs = append(specs, sweepSpec{
			param: "mmr_enabled", value: v,
			mutate: func(o *enhance.Options, value float64) {
				o.MMR = config.MMRConfig{Enabled: value == 1, Lambda: 0.7}
			},
		})
	}
	// Engine-weight sweeps are dataset-specific: anysearch/2, bing/1 and
	// duckduckgo/1 are the baseline weights in the captured corpus.
	for _, v := range []float64{1, 1.5, 2, 2.5} {
		specs = append(specs, sweepSpec{param: "anysearch_weight", value: v, weights: map[string]float64{"anysearch": v}})
	}
	for _, v := range []float64{0.25, 0.5, 1, 2} {
		specs = append(specs, sweepSpec{param: "bing_weight", value: v, weights: map[string]float64{"bing": v}})
	}
	for _, v := range []float64{0.5, 1, 2} {
		specs = append(specs, sweepSpec{param: "duckduckgo_weight", value: v, weights: map[string]float64{"duckduckgo": v}})
	}
	return specs
}

// Markdown renders the sweep as a report table. The best row per parameter is
// marked with `best`; significance is left to the interval column because a
// 20-query corpus has wide confidence bounds.
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Search quality sweep\n\n")
	fmt.Fprintf(&b, "- Dataset: `%s` (%d cases)\n", r.Dataset, r.Cases)
	fmt.Fprintf(&b, "- Baseline: P@8=%.4f R@8=%.4f MRR@8=%.4f\n", r.Baseline.Precision, r.Baseline.Recall, r.Baseline.MRR)
	fmt.Fprintf(&b, "- Paired bootstrap: %d resamples, seed %d\n\n", r.Iterations, r.Seed)
	b.WriteString("| knob | value | P@8 | R@8 | MRR@8 | ΔMRR [95% CI] | W/L/T |\n")
	b.WriteString("|---|---:|---:|---:|---:|---|---:|\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "| `%s` | %g | %.4f | %.4f | %.4f | %+.4f [%+.4f, %+.4f] | %d/%d/%d |\n",
			row.Param, row.Value, row.Agg.Precision, row.Agg.Recall, row.Agg.MRR,
			row.Delta.DeltaMRR, row.Delta.LowMRR, row.Delta.HighMRR,
			row.Delta.Wins, row.Delta.Losses, row.Delta.Ties)
	}
	return b.String()
}
