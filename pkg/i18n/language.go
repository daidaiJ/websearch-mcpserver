package i18n

import (
	"os"
	"strings"
	"sync"

	"golang.org/x/text/language"
)

const (
	// Auto 按系统语言自动适配，也是 config.language 的零值语义。
	Auto = "auto"
	// Zh 中文；En 英文。当前支持的语言集合。
	Zh = "zh"
	En = "en"
)

// supported 支持的语言，顺序即匹配优先级（首项为兜底）。
// 新增语言：加一组 locales/*.<代码>.json，再把标签加进这里。
var supported = []language.Tag{language.Chinese, language.English}

// fallback 系统语言匹配不到受支持语言时使用的语言。
const fallback = En

var (
	matcherOnce sync.Once
	tagMatcher  language.Matcher
)

func matcher() language.Matcher {
	matcherOnce.Do(func() { tagMatcher = language.NewMatcher(supported) })
	return tagMatcher
}

// Normalize 归一配置取值：auto / zh / en；空值与任何无法识别的取值都回退 auto
// （零值 = 安全默认，不要求老配置迁移）。config 加载时用它固化取值。
func Normalize(setting string) string {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case Zh:
		return Zh
	case En:
		return En
	default:
		return Auto
	}
}

// Parse 把配置值解析为受支持语言标签：
// zh / en 显式指定；auto 按系统语言自动适配，匹配不到受支持语言时落 fallback。
func Parse(setting string) language.Tag {
	switch Normalize(setting) {
	case Zh:
		return language.Chinese
	case En:
		return language.English
	default:
		return matchSystem()
	}
}

func matchSystem() language.Tag {
	tag := systemLanguage()
	if tag == language.Und {
		return tagOf(fallback)
	}
	// 取 matcher 命中的受支持标签本身，而不是它回传的原始标签：
	// 系统标签带地区/扩展（如 zh-CN → zh-u-rg-cnzzzz）时归一为 zh / en。
	_, idx, conf := matcher().Match(tag)
	if conf == language.No {
		return tagOf(fallback)
	}
	return supported[idx]
}

func tagOf(code string) language.Tag {
	if code == En {
		return language.English
	}
	return language.Chinese
}

// code 把语言标签归一为受支持语言的代码（zh / en）。
func code(tag language.Tag) string {
	base, _ := tag.Base()
	if base.String() == "en" {
		return En
	}
	return Zh
}

// systemLanguage 探测系统语言：POSIX 环境变量（显式设置优先）→ 平台接口。
// 都拿不到时返回 language.Und，由调用方走 fallback。
func systemLanguage() language.Tag {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" || isPOSIXNeutral(raw) {
			continue
		}
		if tag := parsePOSIXLocale(raw); tag != language.Und {
			return tag
		}
	}
	return systemLocale()
}

// isPOSIXNeutral 过滤 "C" / "POSIX" 这类表示"不指定语言"的取值。
func isPOSIXNeutral(raw string) bool {
	switch strings.ToUpper(raw) {
	case "C", "POSIX":
		return true
	}
	return false
}

// parsePOSIXLocale 解析 POSIX 语言形态：zh_CN.UTF-8@variant → zh-CN。
func parsePOSIXLocale(raw string) language.Tag {
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '@'); i >= 0 {
		raw = raw[:i]
	}
	return language.Make(strings.ReplaceAll(raw, "_", "-"))
}
