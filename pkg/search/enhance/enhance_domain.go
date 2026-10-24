package enhance

import (
	"net/url"
	"regexp"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────────────
// 域名信号：域名品质惩罚 (Domain Quality) + 权威性 Boost (Authority Boost)
// 参考 docs/native-engine-optimization.md 模块二、模块六。
// ──────────────────────────────────────────────────────────────────────────────

const (
	brandPenalty           = 0.2  // 品牌/电商域名惩罚
	mdnElementDriftPenalty = 0.1  // MDN HTML 元素漂移惩罚
	dictionaryErrorPenalty = 0.25 // 词典站在错误码查询时的惩罚
	lowQualityTLDPenalty   = 0.5  // 低质量区域 TLD 惩罚
)

// brandDomains 品牌/电商站，通用检索时误匹配惩罚。
var brandDomains = map[string]struct{}{
	"next.co.uk": {}, "next.us": {}, "next.de": {}, "next.ie": {}, "next.com": {},
	"amazon.com": {}, "amazon.co.uk": {}, "amazon.cn": {}, "walmart.com": {},
	"target.com": {}, "bestbuy.com": {}, "costco.com": {}, "kohls.com": {},
	"macys.com": {}, "nordstrom.com": {}, "jcpenney.com": {}, "ebay.com": {},
	"etsy.com": {}, "aliexpress.com": {}, "wayfair.com": {}, "taobao.com": {},
	"tmall.com": {}, "jd.com": {}, "pinduoduo.com": {},
}

var (
	commercialTLDRe    = regexp.MustCompile(`\.(shop|store|deals|sale|boutique|fashion|wedding|beauty|salon)$`)
	lowQualityTLDRe    = regexp.MustCompile(`\.(tk|cf|ga|gq|ml)$`)
	authoritativeTLDRe = regexp.MustCompile(`\.(io|org|dev|edu|gov)$`)
	dictionaryHostRe   = regexp.MustCompile(
		`(?:^|\.)(?:wiktionary\.org|merriam-webster\.com|dictionary\.com|thesaurus\.com|` +
			`vocabulary\.com|collinsdictionary\.com|dictionary\.cambridge\.org|` +
			`(?:[a-z0-9-]+\.)?(?:dictionary|thesaurus|vocabulary|glossary))\b`)
)

// dbTermRe 常见数据库/存储术语，用于 MDN HTML 元素漂移检测。
var dbTermRe = regexp.MustCompile(`(?i)\b(pgvector|hnsw|postgres|postgresql|redis|mongodb|mongo|mysql|mariadb|sqlite|cockroachdb|cockroach|timescale|elasticsearch|opensearch|clickhouse|duckdb|neon|supabase|planetscale|cassandra|dynamodb|kafka|rabbitmq|qdrant|weaviate|pinecone|milvus|chroma|vespa)\b`)

var mdnElementPathRe = regexp.MustCompile(`/web/html/element/`)

// hostOf 从 URL 提取归一化主机名（去 www. 前缀，小写）。
func hostOf(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}

// DomainQuality 域名品质因子 [0.1, 1.0]，默认 1.0。
func DomainQuality(rawURL, query string, hasErrorToken bool) float64 {
	host := hostOf(rawURL)
	if host == "" {
		return 1.0
	}

	// 品牌/电商域名 + 商业 TLD
	if _, ok := brandDomains[host]; ok {
		return brandPenalty
	}
	if commercialTLDRe.MatchString(host) {
		return brandPenalty
	}

	// MDN HTML 元素漂移：数据库类 query 命中 MDN 元素页 → 强惩罚
	if host == "developer.mozilla.org" && dbTermRe.MatchString(query) {
		lower := strings.ToLower(rawURL)
		if mdnElementPathRe.MatchString(lower) {
			return mdnElementDriftPenalty
		}
	}

	// 词典站：仅错误码查询时惩罚
	if hasErrorToken && dictionaryHostRe.MatchString(host) {
		return dictionaryErrorPenalty
	}

	// 低质量区域 TLD
	if lowQualityTLDRe.MatchString(host) {
		return lowQualityTLDPenalty
	}

	return 1.0
}

// knownDocsHosts 已知官方文档站（权威加分 >= 0.18）。
var knownDocsHosts = map[string]struct{}{
	"docs.python.org": {}, "developer.mozilla.org": {}, "kubernetes.io": {},
	"cloud.google.com": {}, "aws.amazon.com": {}, "docs.aws.amazon.com": {},
	"learn.microsoft.com": {}, "docs.microsoft.com": {}, "developer.apple.com": {},
	"docs.docker.com": {}, "docs.npmjs.com": {}, "docs.github.com": {},
	"docs.anthropic.com": {}, "pkg.go.dev": {}, "go.dev": {}, "docs.oracle.com": {},
	"nginx.org": {}, "docs.spring.io": {},
}

// knownSubjectDomains 已知主题 → 权威域名映射（精确匹配 +0.20，子域 +0.18）。
var knownSubjectDomains = map[string][]string{
	"redis": {"redis.io", "redis.com"}, "postgres": {"postgresql.org", "neon.tech"},
	"postgresql": {"postgresql.org", "neon.tech"}, "mysql": {"mysql.com", "dev.mysql.com"},
	"python": {"python.org", "docs.python.org"}, "react": {"react.dev", "reactjs.org"},
	"nextjs": {"nextjs.org"}, "vue": {"vuejs.org", "cn.vuejs.org"},
	"angular": {"angular.io", "angular.dev"}, "node": {"nodejs.org"}, "nodejs": {"nodejs.org"},
	"rust": {"rust-lang.org", "doc.rust-lang.org"}, "go": {"go.dev", "golang.org"},
	"golang": {"go.dev", "golang.org"}, "typescript": {"typescriptlang.org"},
	"javascript": {"developer.mozilla.org"}, "anthropic": {"anthropic.com", "docs.anthropic.com"},
	"openai": {"openai.com", "platform.openai.com"}, "github": {"github.com", "docs.github.com"},
	"docker": {"docker.com", "docs.docker.com"}, "kubernetes": {"kubernetes.io"}, "k8s": {"kubernetes.io"},
	"aws": {"aws.amazon.com", "docs.aws.amazon.com"}, "azure": {"azure.microsoft.com", "learn.microsoft.com"},
	"gcp": {"cloud.google.com"}, "npm": {"npmjs.com", "docs.npmjs.com"}, "pnpm": {"pnpm.io"},
	"yarn": {"yarnpkg.com"}, "neon": {"neon.tech"}, "gitlab": {"gitlab.com"},
	"microsoft": {"microsoft.com", "learn.microsoft.com"}, "apple": {"apple.com", "developer.apple.com"},
	"java": {"docs.oracle.com", "runoob.com"}, "nginx": {"nginx.org", "nginx.com"},
	"spring": {"spring.io", "docs.spring.io"},
}

// midQualityDocsHosts 中等质量文档/教程站（加分较低）。
var midQualityDocsHosts = map[string]struct{}{
	"runoob.com": {}, "w3school.com.cn": {}, "csdn.net": {}, "cnblogs.com": {},
}

// AuthorityBoost 权威性加分（加性）。
// rareFactor < 0.8（稀有词全缺失）时大幅削弱权威权重，避免误加分。
func AuthorityBoost(query, rawURL string, rareFactor float64) float64 {
	host := hostOf(rawURL)
	if host == "" {
		return 0
	}
	lowerQuery := strings.ToLower(query)
	boost := 0.0

	// 1. 已知主题 → 域名精确/子域匹配
	for subject, domains := range knownSubjectDomains {
		if !strings.Contains(lowerQuery, subject) {
			continue
		}
		for _, d := range domains {
			if host == d {
				boost = max(boost, 0.20)
			} else if strings.HasSuffix(host, "."+d) {
				boost = max(boost, 0.18)
			}
		}
	}

	// 2. 已知官方文档站
	if _, ok := knownDocsHosts[host]; ok {
		boost = max(boost, 0.18)
	}

	// 3. 中等质量文档站
	if _, ok := midQualityDocsHosts[host]; ok {
		boost = max(boost, 0.08)
	}

	// 4. docs. 前缀
	if strings.HasPrefix(host, "docs.") {
		boost = max(boost, 0.08)
	}

	// 5. 权威 TLD
	if authoritativeTLDRe.MatchString(host) {
		boost = max(boost, 0.04)
	}

	// 6. 稀有词 miss 时削弱权威权重
	if rareFactor < 0.8 {
		boost *= 0.25
	}

	return boost
}
