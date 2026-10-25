package enhance

import (
	"regexp"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────────────
// 文本信号：分词、词汇对齐 (Lexical Alignment)、稀有词因子 (Rare Terms Factor)
// 参考 docs/native-engine-optimization.md 模块三、模块四。
// ──────────────────────────────────────────────────────────────────────────────

// stopWords 词汇对齐时忽略的停用词（中英混合场景）。
var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "what": {}, "is": {}, "are": {},
	"was": {}, "were": {}, "how": {}, "why": {}, "when": {}, "where": {},
	"who": {}, "do": {}, "does": {}, "did": {}, "for": {}, "of": {},
	"to": {}, "in": {}, "on": {}, "with": {}, "and": {}, "or": {},
	"but": {}, "as": {}, "at": {}, "by": {}, "from": {}, "into": {},
	"about": {}, "than": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"it": {}, "its": {}, "be": {}, "been": {}, "has": {}, "have": {},
	"had": {}, "can": {}, "could": {}, "should": {}, "would": {}, "may": {},
	"might": {}, "must": {}, "will": {}, "shall": {}, "i": {}, "you": {},
	"we": {}, "they": {}, "he": {}, "she": {}, "them": {}, "my": {},
	"your": {}, "our": {}, "their": {}, "latest": {}, "current": {},
	"newest": {}, "recent": {}, "best": {}, "top": {}, "most": {},
	// 常见中文虚词
	"的": {}, "了": {}, "是": {}, "在": {}, "和": {}, "与": {}, "及": {},
	"如何": {}, "怎么": {}, "什么": {}, "最新": {}, "最近": {},
}

var latinWordRe = regexp.MustCompile(`[a-z0-9]+`)

// isCJK 判断是否为 CJK 表意字符。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一表意
		(r >= 0x3400 && r <= 0x4DBF) || // 扩展 A
		(r >= 0xF900 && r <= 0xFAFF) // 兼容表意
}

// tokenize 将文本切分为词元：
//   - 英文/数字：按非字母数字分割，小写，长度 >= 2
//   - 中文：按字符 bigram（相邻两字），单字作为降级补充
//
// 不依赖分词库，bigram 近似足够用于词汇对齐。
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var toks []string

	// 英文/数字词元
	for _, w := range latinWordRe.FindAllString(s, -1) {
		if len(w) >= 2 {
			toks = append(toks, w)
		}
	}

	// 中文：连续 CJK 段落 → unigram + bigram
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		if len(run) == 1 {
			toks = append(toks, string(run))
		} else {
			for i := 0; i < len(run); i++ {
				toks = append(toks, string(run[i]))
				if i+1 < len(run) {
					toks = append(toks, string(run[i:i+2]))
				}
			}
		}
		run = run[:0]
	}
	for _, r := range s {
		if isCJK(r) {
			run = append(run, r)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

// tokenSet 返回去停用词后的词元集合。
func tokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, t := range tokenize(s) {
		if _, stop := stopWords[t]; stop {
			continue
		}
		set[t] = struct{}{}
	}
	return set
}

// JaccardSimilarity 计算两段文本的 Token Jaccard 相似度 [0,1]。
// 用于 MMR 多样性重排：相似度 = |tokens(a) ∩ tokens(b)| / |tokens(a) ∪ tokens(b)|。
// 空集合或完全无交集返回 0。
func JaccardSimilarity(a, b string) float64 {
	return jaccardSets(tokenSet(a), tokenSet(b))
}

// jaccardSets 计算两个词元集合的 Jaccard 相似度。
func jaccardSets(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if _, ok := b[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// LexicalAlignment 计算 query 与 title+snippet 的词汇重叠率 [0,1]。
// 重叠率 = query 词元中出现在文档词元集合里的比例。
func LexicalAlignment(query, title, snippet string) float64 {
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return 0
	}
	docSet := tokenSet(title + " " + snippet)
	if len(docSet) == 0 {
		return 0
	}

	// query 唯一词元（去停用词）
	seen := make(map[string]struct{})
	var qUnique []string
	for _, t := range qTokens {
		if _, stop := stopWords[t]; stop {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		qUnique = append(qUnique, t)
	}
	if len(qUnique) == 0 {
		return 0
	}

	hit := 0
	for _, t := range qUnique {
		if _, ok := docSet[t]; ok {
			hit++
		}
	}
	la := float64(hit) / float64(len(qUnique))
	if la < 0 {
		la = 0
	} else if la > 1 {
		la = 1
	}
	return la
}

// 复合词形状（无需词典）：连字符 / 下划线 / 字母+数字后缀。
var (
	hyphenWordRe   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)+$`)
	underscoreRe   = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)+$`)
	alphaNumSuffix = regexp.MustCompile(`^[a-z]{2,}\d+$`)
)

// isCompoundTerm 判断是否为技术复合词（稀有词）。
func isCompoundTerm(w string) bool {
	return hyphenWordRe.MatchString(w) || underscoreRe.MatchString(w) || alphaNumSuffix.MatchString(w)
}

// RareTermsFactor 稀有词因子 [0.5, 1.6]。
// 复合词（如 pgvector、fts5_virtual、pg18）命中时提升，全缺失时降低；
// 无复合词时退化为短语连续匹配检测。
func RareTermsFactor(query, title, snippet string) float64 {
	qWords := latinWordRe.FindAllString(strings.ToLower(query), -1)
	doc := strings.ToLower(title + " " + snippet)

	var compounds []string
	for _, w := range qWords {
		if isCompoundTerm(w) {
			compounds = append(compounds, w)
		}
	}

	factor := 1.0
	if len(compounds) > 0 {
		match := 0
		for _, c := range compounds {
			if strings.Contains(doc, c) {
				match++
			}
		}
		if match > 0 {
			factor = 1 + 0.6*float64(match)/float64(len(compounds))
		} else {
			factor = 0.5
		}
	} else {
		// 无复合词：检测 query 短语在文档中的连续匹配
		factor = phraseContinuityFactor(qWords, doc)
	}

	if factor < 0.5 {
		factor = 0.5
	} else if factor > 1.6 {
		factor = 1.6
	}
	return factor
}

// phraseContinuityFactor 检测 query 词序列在文档中的最长连续匹配。
func phraseContinuityFactor(qWords []string, doc string) float64 {
	if len(qWords) < 2 {
		return 1.0
	}
	// 整句连续匹配 → 强提升
	if strings.Contains(doc, strings.Join(qWords, " ")) {
		return 1.4
	}
	// 最长连续子串匹配长度
	best := 1
	for i := 0; i < len(qWords); i++ {
		for j := i + 2; j <= len(qWords); j++ {
			if strings.Contains(doc, strings.Join(qWords[i:j], " ")) {
				if j-i > best {
					best = j - i
				}
			}
		}
	}
	if best <= 1 {
		return 1.0
	}
	return 1 + 0.4*float64(best-1)/float64(len(qWords)-1)
}
