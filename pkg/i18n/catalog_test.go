package i18n

// catalog_test.go — 文案目录审计（先建检测再动手：新增/改动文案漏登记即 fail）。
//
// 三条不变量：
//  1. 每个域在所有受支持语言下的 key 集合完全一致——加语言不会漏条目；
//  2. 同一 key 在各语言下的 {{.占位符}} 集合一致——翻译不会引入运行期伪影；
//  3. 代码引用的 key 与目录里的 key 双向一致——既无漏登记，也无死条目。
//
// 代码侧 key 来源（覆盖四种文案落地形态）：
//   - pkg/log 的 *T helper（日志层）；
//   - i18n 的 T / TCtx 调用（dashboard 响应、MCP 资源正文等）；
//   - jsonschema 结构体标签（MCP 参数描述）；
//   - pkg/dashboard/web 下的 web.* 引用（前端域，数据由 serveIndex 内联注入）。

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// keyDomains 是允许的域前缀，与 locales/<域>.<语言>.json 的文件名一一对应。
var keyDomains = []string{"log", "mcp", "dashboard", "web"}

// loadCatalog 读取内嵌目录，返回 域 → 语言 → key → 文案文本。
func loadCatalog(t *testing.T) map[string]map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]map[string]string{}
	err := fs.WalkDir(localeFS, "locales", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		buf, rerr := fs.ReadFile(localeFS, p)
		if rerr != nil {
			return rerr
		}
		file, perr := goi18n.ParseMessageFileBytes(buf, p, nil)
		if perr != nil {
			return fmt.Errorf("%s: %w", p, perr)
		}
		domain, lang, ok := splitLocaleFile(filepath.Base(p))
		if !ok {
			t.Fatalf("文案文件名不符合 <域>.<语言>.json 约定: %s", p)
		}
		if !slices.Contains(keyDomains, domain) {
			t.Fatalf("未知文案域 %q（文件 %s）：请同步 keyDomains 与文档", domain, p)
		}
		if out[domain] == nil {
			out[domain] = map[string]map[string]string{}
		}
		msgs := make(map[string]string, len(file.Messages))
		for _, m := range file.Messages {
			msgs[m.ID] = messageText(m)
		}
		out[domain][lang] = msgs
		return nil
	})
	if err != nil {
		t.Fatalf("读取内嵌文案目录失败: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("未发现任何文案目录文件")
	}
	return out
}

// splitLocaleFile 解析 dashboard.zh.json → (dashboard, zh)。
func splitLocaleFile(name string) (domain, lang string, ok bool) {
	name = strings.TrimSuffix(name, ".json")
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return "", "", false
	}
	return name[:i], name[i+1:], true
}

// messageText 汇总一条消息的全部复数形态文本。
func messageText(m *goi18n.Message) string {
	parts := []string{m.Zero, m.One, m.Two, m.Few, m.Many, m.Other}
	return strings.Join(parts, "\x00")
}

var placeholderRe = regexp.MustCompile(`\{\{\s*\.([A-Za-z0-9_]+)\s*\}\}`)

// placeholders 返回文案里的占位符名集合（已排序）。
func placeholders(text string) []string {
	var names []string
	for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(names, m[1]) {
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

// TestCatalogParity 校验各语言之间 key 集合与占位符集合一致。
func TestCatalogParity(t *testing.T) {
	cat := loadCatalog(t)
	for domain, langs := range cat {
		base, ok := langs[Zh]
		if !ok {
			t.Errorf("域 %s 缺少 zh 文案文件（zh 是 bundle 默认语言，必须齐全）", domain)
			continue
		}
		for _, lang := range supportedCodes() {
			other, ok := langs[lang]
			if !ok {
				t.Errorf("域 %s 缺少 %s 文案文件", domain, lang)
				continue
			}
			for _, key := range sortedKeys(base) {
				en, ok := other[key]
				if !ok {
					t.Errorf("%s.%s 缺条目: %s", domain, lang, key)
					continue
				}
				if strings.TrimSpace(en) == "" {
					t.Errorf("%s.%s 条目为空: %s", domain, lang, key)
				}
				zhPH, enPH := placeholders(base[key]), placeholders(en)
				if !slices.Equal(zhPH, enPH) {
					t.Errorf("%s.%s 占位符不一致:\n  zh: %v\n  %s: %v", domain, key, zhPH, lang, enPH)
				}
			}
			for _, key := range sortedKeys(other) {
				if _, ok := base[key]; !ok {
					t.Errorf("%s.%s 多出条目（zh 无对应）: %s", domain, lang, key)
				}
			}
		}
		for key := range base {
			domainOf, _, _ := strings.Cut(key, ".")
			if domainOf != domain {
				t.Errorf("key 前缀与文件域不一致: %s 住在 %s.<语言>.json", key, domain)
			}
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func supportedCodes() []string {
	out := make([]string, 0, len(supported))
	for _, tag := range supported {
		out = append(out, code(tag))
	}
	return out
}

// dynamicKeyAllowlist 是允许以非字面量传 key 的文件（纯转发/包装，文案登记在调用方）。
// 新增项必须写清理由；其它任何非字面量 key 都是审计失败。
var dynamicKeyAllowlist = map[string]string{
	"pkg/i18n/i18n.go": "TCtx 是 FromContext(ctx).T(key, kv...) 的转发包装，key 由调用方给出",
	"pkg/log/log.go":   "log.*T 是 i18n.Default().T(key, kv...) 的转发包装，key 由调用方给出",
	"mcp/i18n.go":      "输入 Schema 的 description 就是 key，来自 jsonschema 标签（已单独扫描）",
}

// TestCatalogUsage 双向校验代码引用与目录条目一致。
func TestCatalogUsage(t *testing.T) {
	root := filepath.Join("..", "..")
	cat := loadCatalog(t)

	defined := map[string]string{} // key → 域
	for domain, langs := range cat {
		for key := range langs[Zh] {
			defined[key] = domain
		}
	}

	used, dynamic := collectUsedKeys(t, root)
	for _, d := range dynamic {
		if _, ok := dynamicKeyAllowlist[d.file]; ok {
			continue
		}
		t.Errorf("key 不是字面量，无法静态审计: %s:%d", d.file, d.line)
	}
	for _, key := range sortedKeys(used) {
		if _, ok := defined[key]; !ok {
			t.Errorf("代码引用了目录里不存在的 key: %s（%s）", key, used[key])
		}
	}
	for _, key := range sortedKeys(defined) {
		if _, ok := used[key]; !ok {
			t.Errorf("目录条目无任何代码引用（死条目）: %s.%s", defined[key], key)
		}
	}
}

// dynamicKey 记录一个无法静态判定的 key 调用点。
type dynamicKey struct {
	file string
	line int
}

// collectUsedKeys 扫描代码，返回 key → 首个引用位置，以及无法静态判定的调用点。
func collectUsedKeys(t *testing.T, root string) (used map[string]string, dynamic []dynamicKey) {
	t.Helper()
	used = map[string]string{}
	record := func(key, origin string) {
		if _, ok := used[key]; !ok {
			used[key] = origin
		}
	}

	// Go 源码：*T helper、T/TCtx 调用、jsonschema 标签
	forEachGoSource(t, root, func(fset *token.FileSet, path string, f *ast.File) {
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		logPkg := importName(f, "websearch/pkg/log")
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.StructType:
				collectSchemaKeys(fset, node, rel, record)
			case *ast.CallExpr:
				collectCallKey(fset, node, rel, logPkg, record, &dynamic)
			}
			return true
		})
	})

	// 前端域：pkg/dashboard/web 下的 web.* 引用
	webDir := filepath.Join(root, "pkg", "dashboard", "web")
	entries, err := os.ReadDir(webDir)
	if err != nil {
		t.Fatalf("读取前端目录失败: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".js", ".html":
		default:
			continue
		}
		path := filepath.Join(webDir, e.Name())
		buf, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("读取 %s 失败: %v", path, rerr)
		}
		text := string(buf)
		if strings.HasSuffix(e.Name(), ".js") {
			text = stripJSComments(text)
		}
		for _, key := range webKeyRe.FindAllString(text, -1) {
			record(key, "pkg/dashboard/web/"+e.Name())
		}
	}
	return used, dynamic
}

var webKeyRe = regexp.MustCompile(`\bweb\.[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+`)

// collectCallKey 从 T / TCtx / log.*T 调用里取 key 字面量。
func collectCallKey(fset *token.FileSet, call *ast.CallExpr, rel, logPkg string, record func(string, string), dynamic *[]dynamicKey) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	var idx int
	switch sel.Sel.Name {
	case "T":
		idx = 0
	case "TCtx":
		// TCtx(ctx, key, data)：key 是第二个实参
		idx = 1
	default:
		ident, ok := sel.X.(*ast.Ident)
		if !ok || logPkg == "" || ident.Name != logPkg {
			return
		}
		if !logKeyHelperRe.MatchString(sel.Sel.Name) {
			return
		}
		idx = 0
	}
	if idx >= len(call.Args) {
		return
	}
	pos := fset.Position(call.Args[idx].Pos())
	lit, ok := call.Args[idx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		*dynamic = append(*dynamic, dynamicKey{file: rel, line: pos.Line})
		return
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	record(val, fmt.Sprintf("%s:%d", rel, pos.Line))
}

var logKeyHelperRe = regexp.MustCompile(`^(Debug|Info|Warn|Error|Err)T$`)

// collectSchemaKeys 取 jsonschema 结构体标签里的 key（MCP 参数描述）。
func collectSchemaKeys(fset *token.FileSet, st *ast.StructType, rel string, record func(string, string)) {
	for _, field := range st.Fields.List {
		if field.Tag == nil {
			continue
		}
		raw, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			continue
		}
		tag := reflect.StructTag(raw).Get("jsonschema")
		if tag == "" {
			continue
		}
		pos := fset.Position(field.Pos())
		record(tag, fmt.Sprintf("%s:%d (jsonschema 标签)", rel, pos.Line))
	}
}

// forEachGoSource 遍历仓库内非测试 Go 源码（跳过依赖、构建产物与文档目录）。
func forEachGoSource(t *testing.T, root string, fn func(*token.FileSet, string, *ast.File)) {
	t.Helper()
	skip := map[string]bool{
		".git": true, ".gtmp": true, "node_modules": true, ".handoff": true,
		"docs": true, "web": true, "tmp": true, "latest_logs": true, "issues": true, "data": true,
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}
		fn(fset, path, file)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历仓库源码失败: %v", err)
	}
}

// importName 返回文件里某包的本地标识符（未导入返回空串）。
func importName(f *ast.File, pkgPath string) string {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != pkgPath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" {
				return ""
			}
			return imp.Name.Name
		}
		return filepath.Base(pkgPath)
	}
	return ""
}

// stripJSComments 去掉 JS 行注释与块注释（保留 https:// 这类字面量里的双斜杠）。
func stripJSComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/' && (i == 0 || src[i-1] != ':'):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			i += 2
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// TestLocaleJSONIsFlat 保证文案文件是扁平 key-value：嵌套对象会被 go-i18n 拼成
// 新的 ID，和代码里的 key 对不上，属于静默陷阱，这里直接拦掉。
func TestLocaleJSONIsFlat(t *testing.T) {
	err := fs.WalkDir(localeFS, "locales", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		buf, rerr := fs.ReadFile(localeFS, p)
		if rerr != nil {
			return rerr
		}
		var raw map[string]json.RawMessage
		if uerr := json.Unmarshal(buf, &raw); uerr != nil {
			return fmt.Errorf("%s: %w", p, uerr)
		}
		for key := range raw {
			if strings.Contains(key, " ") || strings.HasPrefix(key, "_") {
				t.Errorf("%s 的 key 形态可疑: %q", p, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("文案文件格式异常: %v", err)
	}
}
