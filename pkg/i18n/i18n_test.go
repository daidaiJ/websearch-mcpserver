package i18n

import (
	"strings"
	"testing"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func TestConfigureAndRender(t *testing.T) {
	t.Cleanup(func() { Configure(Zh) })

	Configure(Zh)
	if got := Default().T("dashboard.tool.smartsearch.label"); got != "智能搜索" {
		t.Fatalf("zh render: %q", got)
	}
	if got := Default().Code(); got != Zh {
		t.Fatalf("zh code: %q", got)
	}

	Configure(En)
	if got := Default().T("dashboard.tool.smartsearch.label"); got != "Smart Search" {
		t.Fatalf("en render: %q", got)
	}
	if got := Default().Code(); got != En {
		t.Fatalf("en code: %q", got)
	}

	got := Default().T("dashboard.suffix.yuanbao_cap", "Limit", 2000)
	if want := "; local monthly cap 2000 calls, calling stops once reached"; got != want {
		t.Fatalf("placeholder render: got %q want %q", got, want)
	}

	if got := Default().T("dashboard.missing.key"); got != "dashboard.missing.key" {
		t.Fatalf("missing key must return the key itself, got %q", got)
	}
}

// TestT_FallsBackToZhWhenEnMissing 钉住 T 的错误处理：go-i18n 在"请求语言缺条目、
// 回退到 bundle 默认语言"时仍会返回可用文案并附带 MessageNotFoundErr，
// T 必须以文案非空为准，否则会把回退文案误判成缺失并露出 key。
func TestT_FallsBackToZhWhenEnMissing(t *testing.T) {
	b := goi18n.NewBundle(language.Chinese)
	if err := b.AddMessages(language.Chinese, &goi18n.Message{ID: "only.zh", Other: "只有中文"}); err != nil {
		t.Fatal(err)
	}
	loc := &Localizer{loc: goi18n.NewLocalizer(b, En), tag: language.English}

	if got := loc.T("only.zh"); got != "只有中文" {
		t.Fatalf("en 缺条目应回退 zh 文案，got %q", got)
	}
	if got := loc.T("nowhere"); got != "nowhere" {
		t.Fatalf("两种语言都缺才返回 key，got %q", got)
	}
}

func TestPluralSelection(t *testing.T) {
	t.Cleanup(func() { Configure(Zh) })
	Configure(En)
	// dashboard 域没有复数条目，这里用包内临时 bundle 校验复数通道可用。
	b := goi18n.NewBundle(language.Chinese)
	err := b.AddMessages(language.English, &goi18n.Message{ID: "n", One: "{{.Count}} result", Other: "{{.Count}} results"})
	if err != nil {
		t.Fatal(err)
	}
	loc := &Localizer{loc: goi18n.NewLocalizer(b, En), tag: language.English}
	if got := loc.T("n", "Count", 1); got != "1 result" {
		t.Fatalf("one: %q", got)
	}
	if got := loc.T("n", "Count", 3); got != "3 results" {
		t.Fatalf("other: %q", got)
	}
}

func TestForAcceptLanguage(t *testing.T) {
	t.Cleanup(func() { Configure(Zh) })
	Configure(Zh)

	cases := []struct {
		header string
		want   string
	}{
		{"", Zh},
		{"en-US,en;q=0.9", En},
		{"en", En},
		{"zh-CN,zh;q=0.9,en;q=0.8", Zh},
		{"fr-FR,fr;q=0.9", Zh}, // 不受支持 → 回退进程默认（zh）
		{"garbage;;;", Zh},
	}
	for _, c := range cases {
		if got := For(c.header).Code(); got != c.want {
			t.Errorf("For(%q) = %q, want %q", c.header, got, c.want)
		}
	}

	Configure(En)
	if got := For("").Code(); got != En {
		t.Errorf("空头应回退进程默认 en，got %q", got)
	}
	if got := For("zh-TW").Code(); got != Zh {
		t.Errorf("zh-TW 应匹配 zh，got %q", got)
	}
}

func TestParse(t *testing.T) {
	for _, c := range []struct {
		setting string
		want    language.Tag
	}{
		{Zh, language.Chinese},
		{"zh", language.Chinese},
		{" ZH ", language.Chinese},
		{En, language.English},
		{"EN", language.English},
	} {
		if got := Parse(c.setting); got != c.want {
			t.Errorf("Parse(%q) = %v, want %v", c.setting, got, c.want)
		}
	}
	// auto / 空值 / 未知值都按系统语言解析，只保证落在受支持集合内。
	for _, setting := range []string{"", Auto, "  ", "klingon"} {
		if got := Parse(setting); got != language.Chinese && got != language.English {
			t.Errorf("Parse(%q) = %v, 应落在受支持语言集合内", setting, got)
		}
	}
}

func TestParsePOSIXLocale(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"zh_CN.UTF-8", "zh-CN"},
		{"en_US", "en-US"},
		{"en_US.UTF-8@euro", "en-US"},
		{"zh", "zh"},
	} {
		got := parsePOSIXLocale(c.raw)
		if got.String() != c.want {
			t.Errorf("parsePOSIXLocale(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
	if !isPOSIXNeutral("C") || !isPOSIXNeutral("POSIX") || isPOSIXNeutral("zh_CN") {
		t.Error("isPOSIXNeutral 判据错误")
	}
}

func TestWithLocalizerRoundTrip(t *testing.T) {
	t.Cleanup(func() { Configure(Zh) })
	Configure(Zh)

	ctx := WithLocalizer(t.Context(), For("en-US"))
	if got := TCtx(ctx, "dashboard.tool.smartsearch.label"); got != "Smart Search" {
		t.Fatalf("TCtx: %q", got)
	}
	if got := TCtx(t.Context(), "dashboard.tool.smartsearch.label"); got != "智能搜索" {
		t.Fatalf("无 context 时应用进程默认: %q", got)
	}
}

func TestRawMessages(t *testing.T) {
	for _, lang := range []string{Zh, En} {
		raw := RawMessages(lang, "dashboard")
		if len(raw) == 0 {
			t.Fatalf("RawMessages(%s, dashboard) 为空", lang)
		}
		if !strings.Contains(string(raw), "dashboard.tool.smartsearch.label") {
			t.Fatalf("RawMessages(%s, dashboard) 内容异常", lang)
		}
	}
	if raw := RawMessages("fr", "dashboard"); raw != nil {
		t.Fatalf("未支持语言应返回 nil，got %q", raw)
	}
	if raw := RawMessages(Zh, "nope"); raw != nil {
		t.Fatalf("未知域应返回 nil，got %q", raw)
	}
}
