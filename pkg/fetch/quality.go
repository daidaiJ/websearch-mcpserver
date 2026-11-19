package fetch

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"websearch/pkg/i18n"
)

// QualityKind 是低质量正文的稳定分类，供调用方决定回退与提示文案。
type QualityKind string

const (
	QualityBotCheck QualityKind = "bot_check" // 人机验证 / 反爬挑战页
	QualityNavOnly  QualityKind = "nav_only"  // 只返回导航链接的壳页
)

// MessageKey 返回该分类对应的 i18n key。
func (k QualityKind) MessageKey() string {
	switch k {
	case QualityBotCheck:
		return "fetch.quality.bot_check"
	case QualityNavOnly:
		return "fetch.quality.nav_only"
	default:
		return ""
	}
}

// Message 返回按当前语言渲染的质量提示；字面量 key 保持在审计可识别处。
func (k QualityKind) Message() string {
	switch k {
	case QualityBotCheck:
		return i18n.T("fetch.quality.bot_check")
	case QualityNavOnly:
		return i18n.T("fetch.quality.nav_only")
	default:
		return ""
	}
}

// ContentQuality 是 markdown 正文的轻量质量画像。所有字段都由纯函数计算，
// 不依赖原始 HTML，因此可以在远端抓取链、本地 webfetch、Jina 与 fetch_top_n
// 共用一个判定口径。
type ContentQuality struct {
	VisibleChars  int
	LinkCount     int
	LinkTextChars int
	LinkTextShare float64
	BotCheck      bool
	NavOnly       bool
}

// Kind 返回低质量分类；正文可用时返回空串。
func (q ContentQuality) Kind() QualityKind {
	if q.BotCheck {
		return QualityBotCheck
	}
	if q.NavOnly {
		return QualityNavOnly
	}
	return ""
}

// LowQuality 报告正文是否应触发回退或显式提示。
func (q ContentQuality) LowQuality() bool {
	return q.Kind() != ""
}

// LowQualityError 让调用方在错误链中识别质量门控结果，同时保留诊断字段。
// Error 文本保持稳定英文，面向用户的本地化文案由生产方通过 i18n 包装。
type LowQualityError struct {
	Kind          QualityKind
	URL           string
	VisibleChars  int
	LinkTextShare float64
}

func (e *LowQualityError) Error() string {
	return "low quality content: " + string(e.Kind)
}

// IsLowQuality 判断错误链是否来自正文质量门控。
func IsLowQuality(err error) bool {
	var target *LowQualityError
	return errors.As(err, &target)
}

var (
	markdownImageRe      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	markdownLinkRe       = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	markdownRefLinkRe    = regexp.MustCompile(`\[([^\]]*)\]\[[^\]]*\]`)
	markdownAutolinkRe   = regexp.MustCompile(`<https?://[^>]+>`)
	bareURLRe            = regexp.MustCompile(`https?://[^\s)>\]]+`)
	htmlTagRe            = regexp.MustCompile(`<[^>]+>`)
	markdownDecorationRe = regexp.MustCompile("[#>*_`~|]+")
)

// AssessContent 评估 markdown 正文质量。阈值按保守方向取值：
// 短文档（例如只有 "Open" 的状态页）仍视为可用，只拦截明确的人机验证页
// 和正文很短的链接导航壳。
func AssessContent(markdown string) ContentQuality {
	var q ContentQuality
	if strings.TrimSpace(markdown) == "" {
		return q
	}

	q.LinkCount, q.LinkTextChars = countLinks(markdown)

	visible := markdownImageRe.ReplaceAllString(markdown, "")
	visible = markdownLinkRe.ReplaceAllString(visible, "$1")
	visible = markdownRefLinkRe.ReplaceAllString(visible, "$1")
	visible = markdownAutolinkRe.ReplaceAllString(visible, "")
	visible = bareURLRe.ReplaceAllString(visible, "")
	visible = htmlTagRe.ReplaceAllString(visible, "")
	visible = markdownDecorationRe.ReplaceAllString(visible, " ")

	q.VisibleChars = visibleLen(visible)
	if q.VisibleChars > 0 {
		q.LinkTextShare = float64(q.LinkTextChars) / float64(q.VisibleChars)
		if q.LinkTextShare > 1 {
			q.LinkTextShare = 1
		}
	}
	q.BotCheck = looksLikeBotCheck(visible, q.VisibleChars)
	q.NavOnly = looksLikeNavOnlyShell(q)
	return q
}

func countLinks(markdown string) (count, textChars int) {
	markdown = markdownImageRe.ReplaceAllString(markdown, "")
	for _, match := range markdownLinkRe.FindAllStringSubmatch(markdown, -1) {
		target := strings.TrimSpace(match[2])
		if target == "" || strings.HasPrefix(target, "#") {
			continue
		}
		text := cleanLinkText(match[1])
		if text == "" {
			continue
		}
		count++
		textChars += visibleLen(text)
	}
	for _, match := range markdownRefLinkRe.FindAllStringSubmatch(markdown, -1) {
		text := cleanLinkText(match[1])
		if text == "" {
			continue
		}
		count++
		textChars += visibleLen(text)
	}
	return count, textChars
}

func cleanLinkText(text string) string {
	text = markdownImageRe.ReplaceAllString(text, "")
	text = htmlTagRe.ReplaceAllString(text, "")
	text = markdownDecorationRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func visibleLen(text string) int {
	n := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

func looksLikeBotCheck(visible string, visibleChars int) bool {
	// 长文里出现 "captcha" 等词通常是在讨论验证码；只对短页启用关键词判定。
	if visibleChars == 0 || visibleChars > 2500 {
		return false
	}
	lower := strings.ToLower(visible)
	for _, keyword := range []string{
		"just a moment",
		"checking your browser",
		"verify you are human",
		"are you a human",
		"complete the captcha",
		"attention required",
		"enable javascript",
		"requires javascript",
		"access denied",
		"challenge-platform",
		"安全验证",
		"人机验证",
		"访问异常",
		"验证码",
		"请启用 javascript",
		"请启用js",
	} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func looksLikeNavOnlyShell(q ContentQuality) bool {
	if q.VisibleChars < 15 || q.VisibleChars > 15000 || q.LinkCount < 3 {
		return false
	}
	if q.LinkTextShare < 0.6 {
		return false
	}
	nonLinkChars := q.VisibleChars - q.LinkTextChars
	return nonLinkChars <= 800
}
