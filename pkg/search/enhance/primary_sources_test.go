package enhance

import (
	"math"
	"testing"

	"websearch/pkg/search/core"
)

func TestHasDataIntent(t *testing.T) {
	yes := []string{
		"2024 年中国 GDP 数据",
		"US population census 2020",
		"world bank statistics on poverty",
		"official carbon emissions data",
		"中国 出口额 统计公报",
		"IMF inflation indicators",
		"上市公司 年报 数据集",
	}
	no := []string{
		"Go generics performance",
		"redis persistence",
		"how to parse json",
		"数据库 性能 报告",
		"latest kubernetes release",
	}
	for _, query := range yes {
		if !HasDataIntent(query) {
			t.Errorf("should detect data intent: %q", query)
		}
	}
	for _, query := range no {
		if HasDataIntent(query) {
			t.Errorf("should not detect data intent: %q", query)
		}
	}
}

func TestPrimarySourceBoost(t *testing.T) {
	tests := []struct {
		name  string
		query string
		url   string
		want  float64
	}{
		{"China statistics bureau", "2024 中国 GDP 数据", "https://www.stats.gov.cn/sj/zxfb/", 0.22},
		{"World Bank data subdomain", "population statistics", "https://data.worldbank.org/indicator/SP.POP.TOTL", 0.22},
		{"IMF", "inflation indicators", "https://www.imf.org/en/Data", 0.22},
		{"Census", "population census", "https://www.census.gov/data.html", 0.20},
		{"Generic .gov", "carbon emissions data", "https://www.epa.gov/ghgemissions", 0.16},
		{"Generic .gov.cn", "出口额 统计", "https://www.example.gov.cn/data", 0.20},
		{"Commercial aggregator", "GDP data", "https://www.statista.com/outlook", 0},
		{"Non-data query on primary source", "redis persistence", "https://data.worldbank.org/", 0},
		{"Random blog", "GDP data", "https://random-blog.net/gdp", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrimarySourceBoost(tt.query, tt.url)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("PrimarySourceBoost(%q, %q) = %v, want %v", tt.query, tt.url, got, tt.want)
			}
		})
	}
}

func TestAuthorityBoostDataIntentKeepsPrimarySource(t *testing.T) {
	// A data query with weak lexical overlap still keeps half of the official
	// source boost, while a non-data query gets nothing from the same host.
	data := AuthorityBoost("2024 中国 GDP 数据", "https://www.stats.gov.cn/sj/zxfb/", 0.5)
	if data < 0.10 {
		t.Fatalf("official source should survive the rare-term gate: %v", data)
	}
	if got := AuthorityBoost("redis persistence", "https://www.stats.gov.cn/sj/zxfb/", 0.5); got != 0 {
		t.Fatalf("non-data query should not get a primary-source boost: %v", got)
	}
}

func TestAuthorityBoostPrimarySourceToggle(t *testing.T) {
	const (
		query = "2024 中国 GDP 数据"
		url   = "https://www.stats.gov.cn/sj/zxfb/"
	)
	if got := AuthorityBoostWithPrimarySources(query, url, 1.0, true); got < 0.22 {
		t.Fatalf("enabled primary-source boost = %v, want >= 0.22", got)
	}
	if got := AuthorityBoostWithPrimarySources(query, url, 1.0, false); got != 0 {
		t.Fatalf("disabled primary-source boost = %v, want 0", got)
	}
	if !DefaultOptions().PrimarySourceBoost {
		t.Fatal("DefaultOptions should enable primary-source boost")
	}
}

func TestEnhanceResultsPrimarySourceOption(t *testing.T) {
	buckets := []core.ScoreBucket{{
		Name:   "bing",
		Weight: 1,
		Results: []core.SearchResult{
			{Title: "2024 年中国 GDP 数据", Url: "https://www.stats.gov.cn/sj/zxfb/", Content: "2024 年中国 GDP 数据 统计公报"},
			{Title: "2024 GDP data overview", Url: "https://example.com/gdp", Content: "2024 GDP data overview"},
		},
	}}
	const query = "2024 中国 GDP 数据"
	scoreOf := func(results []core.SearchResult, url string) float64 {
		for _, r := range results {
			if r.Url == url {
				return r.Score
			}
		}
		t.Fatalf("result %s missing from %v", url, results)
		return 0
	}

	opts := DefaultOptions()
	opts.Threshold = 0.0001
	enabled := EnhanceResultsWithOptions(query, buckets, opts)
	opts.PrimarySourceBoost = false
	disabled := EnhanceResultsWithOptions(query, buckets, opts)

	onScore := scoreOf(enabled, "https://www.stats.gov.cn/sj/zxfb/")
	offScore := scoreOf(disabled, "https://www.stats.gov.cn/sj/zxfb/")
	if onScore <= offScore {
		t.Fatalf("primary-source boost should raise the official source score: enabled=%v disabled=%v", onScore, offScore)
	}
}
