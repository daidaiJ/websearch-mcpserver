package quality

import (
	"net/url"
	"strings"
	"testing"

	"websearch/pkg/search/core"
	"websearch/pkg/search/enhance"
)

const topK = 8

func buildBuckets(tc qualityCase) []core.ScoreBucket {
	d, e := tc.Distractors, tc.Expected
	pad := func(list []core.SearchResult, n int) []core.SearchResult {
		for len(list) < n {
			list = append(list, list[len(list)%len(tc.Distractors)])
		}
		return list
	}
	d, e = pad(d, 4), pad(e, 2)
	return []core.ScoreBucket{
		{Name: "bing", Weight: 1, Results: []core.SearchResult{d[0], e[0], d[1], e[1], d[2], d[3]}},
		{Name: "ddg", Weight: 1, Results: []core.SearchResult{e[0], d[0], e[1], d[1], d[2], d[3]}},
	}
}

func expectedDomains(results []core.SearchResult) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(results))
	for _, r := range results {
		host := hostOf(r.Url)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	return out
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// score returns precision@k over returned results, recall over distinct
// expected domains, MRR of the first relevant result, the hit count and the
// first relevant rank (1-based, 0 when absent).
func score(results []core.SearchResult, domains []string) (precision, recall, mrr float64, hits, first int) {
	n := len(results)
	if n > topK {
		n = topK
	}
	matched := map[string]struct{}{}
	for i := 0; i < n; i++ {
		host := hostOf(results[i].Url)
		for _, want := range domains {
			if host == want || strings.HasSuffix(host, "."+want) {
				hits++
				matched[want] = struct{}{}
				if first == 0 {
					first = i + 1
				}
				break
			}
		}
	}
	if n > 0 {
		precision = float64(hits) / float64(n)
	}
	if len(domains) > 0 {
		recall = float64(len(matched)) / float64(len(domains))
	}
	if first > 0 {
		mrr = 1 / float64(first)
	}
	return precision, recall, mrr, hits, first
}

func TestFixturesAreWellFormed(t *testing.T) {
	cases := fixtures()
	if len(cases) != 20 {
		t.Fatalf("fixtures = %d, want 20 (10 English + 10 Chinese)", len(cases))
	}
	names := map[string]struct{}{}
	for _, tc := range cases {
		if tc.Name == "" || tc.Query == "" {
			t.Fatalf("case missing name/query: %+v", tc)
		}
		if _, ok := names[tc.Name]; ok {
			t.Fatalf("duplicate case name %q", tc.Name)
		}
		names[tc.Name] = struct{}{}
		if len(tc.Expected) < 2 {
			t.Fatalf("%s: expected %d results, want at least 2", tc.Name, len(tc.Expected))
		}
		if len(tc.Distractors) < 4 {
			t.Fatalf("%s: distractors %d, want at least 4", tc.Name, len(tc.Distractors))
		}
		urls := map[string]struct{}{}
		for _, r := range append(append([]core.SearchResult{}, tc.Expected...), tc.Distractors...) {
			if r.Title == "" || r.Url == "" {
				t.Fatalf("%s: candidate missing title/url: %+v", tc.Name, r)
			}
			if _, ok := urls[r.Url]; ok {
				t.Fatalf("%s: duplicate candidate URL %s", tc.Name, r.Url)
			}
			urls[r.Url] = struct{}{}
		}
	}
}

func TestOfflineRankingQuality(t *testing.T) {
	cases := fixtures()
	var sumP, sumR, sumMRR float64
	for _, tc := range cases {
		got := enhance.EnhanceResults(tc.Query, buildBuckets(tc), 0, topK)
		domains := expectedDomains(tc.Expected)
		p, rec, mrr, hits, first := score(got, domains)
		sumP += p
		sumR += rec
		sumMRR += mrr

		t.Logf("%-28s P@%d=%.2f R@%d=%.2f MRR@%d=%.2f hits=%d first=%d",
			tc.Name, topK, p, topK, rec, topK, mrr, hits, first)
		if hits < tc.MinHits {
			t.Errorf("%s: expected-domain hits = %d, want at least %d (ranked: %s)", tc.Name, hits, tc.MinHits, rankedHosts(got))
		}
		if first == 0 || first > tc.MaxFirstRank {
			t.Errorf("%s: first expected domain rank = %d, want 1..%d (ranked: %s)", tc.Name, first, tc.MaxFirstRank, rankedHosts(got))
		}
	}

	meanP, meanR, meanMRR := sumP/float64(len(cases)), sumR/float64(len(cases)), sumMRR/float64(len(cases))
	t.Logf("mean P@%d=%.2f R@%d=%.2f MRR@%d=%.2f", topK, meanP, topK, meanR, topK, meanMRR)
	if meanP < 0.30 || meanR < 0.90 || meanMRR < 0.85 {
		t.Fatalf("aggregate quality regressed: P=%.2f R=%.2f MRR=%.2f", meanP, meanR, meanMRR)
	}
}

func rankedHosts(results []core.SearchResult) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		parts = append(parts, hostOf(r.Url))
	}
	return strings.Join(parts, " > ")
}
