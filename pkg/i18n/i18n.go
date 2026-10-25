// Package i18n 提供进程级与每请求的语言解析、文案渲染与文案目录治理。
//
// 设计（对齐 Go 后端 i18n 常规做法）：
//   - 文案数据住 pkg/i18n/locales/<域>.<语言>.json，经 go:embed 打进二进制；
//     新增一门语言 = 新增一组同域文件，新增文案 = 新增一个 key，都不需要改 Go 源码；
//   - key 是符号化稳定标识（log.cache.cleanup_started / mcp.tool.smartsearch.desc /
//     dashboard.source.baidu.name / web.nav.overview），改文案不动 key、不动代码；
//   - 语言标签走 golang.org/x/text/language：配置值 auto/zh/en 解析为 BCP-47 标签，
//     HTTP 面按 Accept-Language 协商，协商不出回退配置解析结果；
//   - 渲染走 nicksnyder/go-i18n：占位符是命名模板 {{.Name}}，en 缺条目时回退 zh
//     （bundle 默认语言），两者都缺才返回 key 本身——缺条目由 i18n_test.go 的
//     目录审计拦截，不靠运行期兜底。
//
// 三种生命周期各取所需：
//   - 日志：进程默认语言（Configure 注入，见 pkg/log）；
//   - MCP 工具描述与参数 Schema：注册时固化，切换语言需重启进程；
//   - HTTP（控制中心 / SearXNG 兼容端点）：每请求协商，见 HTTP 中间件。
package i18n

import (
	"context"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Localizer 绑定一种语言，负责按 key 渲染文案。
type Localizer struct {
	loc *goi18n.Localizer
	tag language.Tag
}

// T 渲染 key 对应的文案。kv 是成对出现的模板数据（名称, 值），对应文案里的
// {{.名称}} 占位符；保留名称 Count（整数）同时作为 CLDR 复数计数。
//
// 成对性是静态可查的（catalog_test.go 会校验每个调用点的实参个数为偶数），
// 名称非字符串的对在运行期忽略，不 panic。
//
// go-i18n 在"请求语言缺条目、回退到 bundle 默认语言"时仍会返回可用文案并附带
// MessageNotFoundErr，因此这里以文案非空为准：en 缺条目回退 zh，zh 也缺才返回
// key 本身（漏登记由目录审计拦截，不靠运行期兜底）。
func (l *Localizer) T(key string, kv ...any) string {
	cfg := &goi18n.LocalizeConfig{MessageID: key}
	if data := templateData(kv); len(data) > 0 {
		cfg.TemplateData = data
		if count, ok := data["Count"]; ok {
			cfg.PluralCount = count
		}
	}
	msg, _ := l.loc.Localize(cfg)
	if msg == "" {
		return key
	}
	return msg
}

// templateData 把成对的 (名称, 值) 实参整理为模板数据；名称非字符串的对忽略。
func templateData(kv []any) map[string]any {
	if len(kv) < 2 {
		return nil
	}
	data := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		name, ok := kv[i].(string)
		if !ok {
			continue
		}
		data[name] = kv[i+1]
	}
	return data
}

// Code 返回语言代码（zh / en），用于日志、响应字段与前端资源选择。
func (l *Localizer) Code() string { return code(l.tag) }

// Tag 返回语言标签。
func (l *Localizer) Tag() language.Tag { return l.tag }

func newLocalizer(tag language.Tag) *Localizer {
	return &Localizer{loc: goi18n.NewLocalizer(bundle(), tag.String()), tag: tag}
}

var (
	mu     sync.RWMutex
	defLoc *Localizer

	fallbackOnce sync.Once
	fallbackLoc  *Localizer
)

// Configure 解析配置值（auto / zh / en，空值等价 auto）并设为进程默认语言。
// 入口在 config.Load 之后调用一次；进程内重复调用以最后一次为准。
func Configure(setting string) {
	loc := newLocalizer(Parse(setting))
	mu.Lock()
	defLoc = loc
	mu.Unlock()
}

// Default 返回进程默认 Localizer。未显式 Configure（如库用法与测试）时，
// 按系统语言即时解析并缓存，行为与 language: auto 一致。
func Default() *Localizer {
	mu.RLock()
	loc := defLoc
	mu.RUnlock()
	if loc != nil {
		return loc
	}
	fallbackOnce.Do(func() { fallbackLoc = newLocalizer(Parse(Auto)) })
	return fallbackLoc
}

// For 按 Accept-Language 头协商语言：能匹配到受支持语言就用它，
// 否则回退进程默认。空串、非法头、全部不被支持都走回退。
func For(acceptLanguage string) *Localizer {
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(tags) == 0 {
		return Default()
	}
	// 命中后取受支持标签本身，保持 Localizer 的语言标签稳定（zh / en）。
	_, idx, conf := matcher().Match(tags...)
	if conf == language.No {
		return Default()
	}
	return newLocalizer(supported[idx])
}

type ctxKey struct{}

// WithLocalizer 把 Localizer 放进 context，供下游按请求取用。
func WithLocalizer(ctx context.Context, loc *Localizer) context.Context {
	return context.WithValue(ctx, ctxKey{}, loc)
}

// FromContext 取 context 里的 Localizer，缺失时返回进程默认。
func FromContext(ctx context.Context) *Localizer {
	if loc, ok := ctx.Value(ctxKey{}).(*Localizer); ok && loc != nil {
		return loc
	}
	return Default()
}

// T 是 Default().T(key, kv...) 的简写，供进程级语言面使用（日志、MCP 注册）。
// 需要按请求协商语言的 HTTP handler 请用 TCtx / FromContext。
func T(key string, kv ...any) string { return Default().T(key, kv...) }

// TCtx 是 FromContext(ctx).T(key, kv...) 的简写。
func TCtx(ctx context.Context, key string, kv ...any) string {
	return FromContext(ctx).T(key, kv...)
}
