package enhance

import (
	"regexp"
	"strings"
	"testing"
)

// latinWordRe 旧实现的正则参照：生产代码已换手写扫描（asciiWords），
// 这里保留作等价性 oracle，两实现不一致时测试失败。
var latinWordRe = regexp.MustCompile(`[a-z0-9]+`)

// latinTokens 旧 tokenize 的完整复刻（tokenize 的英文/数字分支）：
// 小写后按 `[a-z0-9]+` 切词、长度 >= 2 保留，CJK 分支逐字节一致故不参与对比。
func latinTokens(s string) []string {
	s = strings.ToLower(s)
	var toks []string
	for _, w := range latinWordRe.FindAllString(s, -1) {
		if len(w) >= 2 {
			toks = append(toks, w)
		}
	}
	return toks
}

// tokenizeEquivalenceCases 等价性语料：中英混排、CJK 长串、数字、连字符、
// 下划线、emoji、ASCII 边界、空串、全分隔符、超长 CJK。
var tokenizeEquivalenceCases = []string{
	"",
	"   \t\n  ",
	"go generics performance",
	"Go Generics 性能调优指南",
	"泛型性能",
	"数据库索引调优 database index tuning 2024",
	"a I x 1 22 333",
	"pgvector fts5_virtual pg18 rust-lang node_js",
	"foo--bar__baz 123-456 abcDEF",
	"0123456789",
	"https://example.com/path?q=1&r=2",
	"python3.12 is_out 😀🎉 emoji test",
	"中文English日本語한국어mixed",
	"\x00\x01\x7f\x80\xfe\xff",
	"ａｂｃ－ＸＹＺ", // 全角字符（非 ASCII）
	"Ǆǈǅǅ ＡＢ", // 非ASCII 大小写折叠会产生多字节序列的字符
	"aaa bbb\tccc\r\nddd\n\neee",
	"!!!???###$$$%%%^^^&&&***((((')))***@@@~~~",
	"\u4e00\u4e01\u4e02",               // 单字符 CJK
	strings.Repeat("数据库索引调优实战手册", 200), // 超长 CJK（含换行分隔符）
	strings.Repeat("go performance tuning ", 100) + strings.Repeat("中文性能测试", 100),
	"\u3400\u4dbf\uf900\ufaff", // isCJK 各区间边界
}

// TestTokenizeEquivalenceWithRegexOracle 新手写扫描实现与旧正则实现的
// 字节级等价：逐条语料断言 tokenize 的 ASCII 词元分支与旧 oracle 一致。
// 旧实现改回生产代码时本用例仍通过（等价性由语料固定，不由实现替代）。
func TestTokenizeEquivalenceWithRegexOracle(t *testing.T) {
	for i, s := range tokenizeEquivalenceCases {
		// tokenize 整体输出 = oracle 词元（长度>=2 过滤后）+ 同一 CJK 分支输出，
		// 两者共享 CJK 实现，故逐 token 断言 ASCII 分支等价即锁定整体语义。
		got := tokenize(s)
		want := latinTokens(s)
		// oracle 词元应按顺序逐个出现在 tokenize 输出的前缀位置
		if len(got) < len(want) {
			t.Fatalf("语料 %d %q: tokenize 输出短于 oracle 词元: got=%v want=%v", i, s, got, want)
		}
		for j, w := range want {
			if got[j] != w {
				t.Errorf("语料 %d %q: 第 %d 个词元不等价, got %q want %q\n完整 got=%v", i, s, j, got[j], w, got)
			}
		}
		// tokenize 中 ASCII 词元与 oracle 词元应一一对应（去掉 CJK 分支产物后集合一致）
		var asciiGot []string
		for _, tok := range got {
			isCJKTok := false
			for _, r := range tok {
				if isCJK(r) {
					isCJKTok = true
					break
				}
			}
			if !isCJKTok {
				asciiGot = append(asciiGot, tok)
			}
		}
		if len(asciiGot) != len(want) {
			t.Errorf("语料 %d %q: ASCII 词元数不等, got=%v want=%v", i, s, asciiGot, want)
		}
	}
}

// TestAsciiWordsDirect 直接断言手写扫描：分隔符语义与顺序（长度 >= 2 的过滤
// 是 tokenize 的职责，asciiWords 本身不过滤长度）。
func TestAsciiWordsDirect(t *testing.T) {
	cases := map[string][]string{
		"":                      {},
		"!!!,,,":                {},
		"Go Generics":           {"go", "generics"},
		"a b c d":               {"a", "b", "c", "d"},
		"ab":                    {"ab"},
		"x1y2 z3":               {"x1y2", "z3"},
		"foo-bar_baz9":          {"foo", "bar", "baz9"},
		"中文GO中文":                {"go"}, // CJK rune 之外按分隔符处理
		"emoji😀abc🎉test":        {"emoji", "abc", "test"},
		"\x00a\x01b\x7fc":       {"a", "b", "c"},
		"aaa\x80bbb\xfeccc\xff": {"aaa", "bbb", "ccc"},
	}
	for in, want := range cases {
		if got := asciiWords(strings.ToLower(in)); !equalStrings(got, want) {
			t.Errorf("asciiWords(%q) = %v, want %v", in, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BenchmarkTokenize tokenize 基线：中英混排语料，供去正则前后对照。
func BenchmarkTokenize(b *testing.B) {
	s := "Go Generics 性能调优指南 database index tuning pgvector fts5_virtual pg18 中文English日本語mixed https://example.com/path?q=1"
	b.ReportAllocs()
	for b.Loop() {
		tokenize(s)
	}
}
