package enhance

import (
	"regexp"
	"strings"
)

// Data/statistics queries benefit from a source model that is broader than the
// tech-docs table in enhance_domain.go: official statistics bureaus, central
// banks, international organisations and government open-data portals. The
// boost stays intent-gated so a general query is not pulled toward a random
// .gov page.

var dataIntentRe = regexp.MustCompile(`(?i)(` +
	`\b(gdp|gni|gini|inflation|cpi|ppi|population|census|statistics?|statistical|dataset|data\s+set|` +
	`indicators?|unemployment|exports?|imports?|trade\s+balance|emissions?|co2|carbon\s+emissions|` +
	`poverty|literacy|life\s+expectancy|fertility|mortality|foreign\s+direct\s+investment|fdi|` +
	`official\s+report|white\s+paper|annual\s+report|statistical\s+bulletin)\b|` +
	`国内生产总值|国民收入|通胀|通货膨胀|人口|普查|统计|指标|数据集|官方数据|统计数据|` +
	`出口额|进口额|出口量|进口量|贸易额|失业率|碳排放|人均|贫困率|识字率|预期寿命|` +
	`出生率|死亡率|外商直接投资|统计公报|白皮书|年报|政策文件` +
	`)`)

// HasDataIntent reports whether a query asks for statistical, economic or
// official data. The signal is deliberately narrow: bare "data" or "report"
// would fire on ordinary programming and documentation queries.
func HasDataIntent(query string) bool {
	return dataIntentRe.MatchString(query)
}

// primarySource is a domain family with a fixed authority boost. Suffix
// matching covers data subdomains such as data.worldbank.org.
type primarySource struct {
	host  string
	boost float64
}

// The list is intentionally small and stable; the query gate, not a huge
// catalogue, is what keeps the signal precise. Extend it when a source family
// proves repeatedly useful in real queries.
var primarySourceHosts = []primarySource{
	// Chinese official statistics and regulators.
	{"stats.gov.cn", 0.22},
	{"pbc.gov.cn", 0.22},
	{"customs.gov.cn", 0.22},
	{"mof.gov.cn", 0.22},
	{"ndrc.gov.cn", 0.20},
	{"moa.gov.cn", 0.20},
	{"nhc.gov.cn", 0.20},
	{"miit.gov.cn", 0.20},
	{"csrc.gov.cn", 0.18},
	{"safe.gov.cn", 0.18},
	{"hkexnews.hk", 0.16},
	{"cninfo.com.cn", 0.14},
	{"gov.cn", 0.20},

	// International organisations and official data portals.
	{"worldbank.org", 0.22},
	{"imf.org", 0.22},
	{"oecd.org", 0.20},
	{"who.int", 0.20},
	{"un.org", 0.18},
	{"fao.org", 0.18},
	{"unesco.org", 0.18},
	{"wto.org", 0.18},
	{"iea.org", 0.18},
	{"ec.europa.eu", 0.18},
	{"eurostat.ec.europa.eu", 0.20},

	// US and other official statistics portals.
	{"census.gov", 0.20},
	{"bls.gov", 0.20},
	{"bea.gov", 0.20},
	{"fred.stlouisfed.org", 0.18},
	{"data.gov", 0.18},
	{"cdc.gov", 0.18},
	{"noaa.gov", 0.18},
	{"eia.gov", 0.18},
	{"nasa.gov", 0.16},
	{"sec.gov", 0.16},

	// Research aggregators with transparent methodology.
	{"ourworldindata.org", 0.12},
}

// PrimarySourceBoost returns the intent-gated authority boost for a URL.
// Non-data queries always return 0, including URLs on the list above.
func PrimarySourceBoost(query, rawURL string) float64 {
	if !HasDataIntent(query) {
		return 0
	}
	host := hostOf(rawURL)
	if host == "" {
		return 0
	}
	best := 0.0
	for _, source := range primarySourceHosts {
		if host == source.host || strings.HasSuffix(host, "."+source.host) {
			best = max(best, source.boost)
		}
	}
	// Generic government suffixes cover agencies that are not in the list.
	if strings.HasSuffix(host, ".gov.cn") || host == "gov.cn" {
		best = max(best, 0.20)
	}
	if strings.HasSuffix(host, ".gov") {
		best = max(best, 0.16)
	}
	return best
}
