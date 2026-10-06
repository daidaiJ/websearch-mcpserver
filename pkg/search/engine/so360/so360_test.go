package so360

import (
	"strings"
	"testing"
	"time"
	"websearch/internal/testenv"

	"websearch/pkg/antirobot"
)

// 解析契约基于 2026-10-06 出口实测的 so.com 结果页结构（li.res-list + data-mdurl）。
const so360FixtureHTML = `<!DOCTYPE html><html><body><ul class="result">
<li class="res-list"><div id="mohe-sv" class="g-mohe" data-mohe-type="short_video_vertical">
<h3 class="g-title"><a href="https://www.so.com/link?m=aaa" data-mdurl="https://tv.360kan.com/s?q=x">golang-短视频大全</a></h3></div></li>
<li class="res-list"><h3 class="res-title"><a href="https://www.so.com/link?m=bbb" data-mdurl="https://go.dev/doc/go1.26">Go 1.26 <em>release</em> notes</a></h3>
<p class="res-desc">Go 1.26 已发布，包含 <em>release</em> 各项更新与下载地址。</p></li>
<li class="res-list"><h3 class="res-title"><a href="https://blog.example.com/post">纯直链条目</a></h3>
<p class="res-desc">第二个结果的摘要文本。</p></li>
<li class="res-list"><h3 class="res-title"><a href="https://www.so.com/link?m=ccc">无 mdurl 的包装链接</a></h3></li>
</ul></body></html>`

func newTestEngine(blocked []string) antirobot.Engine {
	return NewSo360(So360Opts{Enabled: true, Blocked: blocked})
}

func TestSo360_ParseResults(t *testing.T) {
	e := newTestEngine(nil).(*so360Engine)
	results := e.parseResults(so360FixtureHTML)

	// mohe 特型卡与无 mdurl 的 /link 包装被丢弃，剩 2 条自然结果
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.URL != "https://go.dev/doc/go1.26" {
		t.Errorf("data-mdurl 应优先于 so.com/link 包装 href, got %q", r.URL)
	}
	if r.Title != "Go 1.26 release notes" {
		t.Errorf("标题应去除高亮 em 标签, got %q", r.Title)
	}
	if !strings.Contains(r.Content, "各项更新与下载地址") {
		t.Errorf("应取 p.res-desc 摘要, got %q", r.Content)
	}
	if r.Engine != "so360" || r.Type != antirobot.ResultWeb {
		t.Errorf("engine/type 标注不符: %+v", r)
	}
	if results[1].URL != "https://blog.example.com/post" {
		t.Errorf("直链条目应保留, got %q", results[1].URL)
	}
}

func TestSo360_BuildURL(t *testing.T) {
	e := newTestEngine(nil).(*so360Engine)

	u := e.buildURL("golang 教程", 1, antirobot.TimeRangeNone)
	if !strings.HasPrefix(u, "https://www.so.com/s?") {
		t.Fatalf("base url mismatch: %s", u)
	}
	if !strings.Contains(u, "q=golang+%E6%95%99%E7%A8%8B") && !strings.Contains(u, "q=golang+教") {
		t.Errorf("query 应编码进 q 参数: %s", u)
	}
	if !strings.Contains(u, "rn=10") {
		t.Errorf("应携带 rn=10: %s", u)
	}
	if strings.Contains(u, "secure=") || strings.Contains(u, "adv_t=") || strings.Contains(u, "pn=") {
		t.Errorf("默认页不应携带 secure/adv_t/pn: %s", u)
	}

	u2 := e.buildURL("golang", 2, antirobot.TimeRangeWeek)
	if !strings.Contains(u2, "adv_t=w") {
		t.Errorf("week 应映射 adv_t=w: %s", u2)
	}
	if !strings.Contains(u2, "pn=2") {
		t.Errorf("第 2 页应携带 pn=2: %s", u2)
	}

	e2 := NewSo360(So360Opts{Enabled: true, SafeSearch: 1}).(*so360Engine)
	if u3 := e2.buildURL("q", 1, antirobot.TimeRangeNone); !strings.Contains(u3, "secure=1") {
		t.Errorf("SafeSearch 开启应携带 secure=1: %s", u3)
	}
}

func TestSo360_TimeRangeCode(t *testing.T) {
	cases := map[antirobot.TimeRange]string{
		antirobot.TimeRangeDay:   "d",
		antirobot.TimeRangeWeek:  "w",
		antirobot.TimeRangeMonth: "m",
		antirobot.TimeRangeYear:  "y",
		antirobot.TimeRangeNone:  "",
	}
	for tr, want := range cases {
		if got := so360TimeRangeCode(tr); got != want {
			t.Errorf("so360TimeRangeCode(%v) = %q, want %q", tr, got, want)
		}
	}
}

func TestSo360_FilterBlocked(t *testing.T) {
	e := newTestEngine([]string{"spam.example"}).(*so360Engine)
	results := []antirobot.Result{
		{Title: "a", URL: "https://spam.example/x"},
		{Title: "b", URL: "https://sub.spam.example/y"},
		{Title: "c", URL: "https://good.example/z"},
	}
	out := e.filterBlocked(results)
	if len(out) != 1 || out[0].URL != "https://good.example/z" {
		t.Fatalf("host 精确与子域匹配应被屏蔽: %+v", out)
	}
}

func TestSo360_CaptchaDetection(t *testing.T) {
	if !so360CaptchaRe.MatchString(`<script src="https://wappass.so.com/captcha.js">`) {
		t.Fatal("wappass 验证码页应被识别为反爬拦截")
	}
	if so360CaptchaRe.MatchString(`<ul class="result"><li class="res-list">正常结果页`) {
		t.Fatal("正常结果页不应误判为验证码")
	}
}

// TestSo360_SearchIntegration 真网集成：解析契约对真实结果页生效
// （data-mdurl 回源、mohe 卡过滤、摘要非空）。网络场景自动跳过。
func TestSo360_SearchIntegration(t *testing.T) {
	testenv.Require(t, testenv.So360)
	e := NewSo360(So360Opts{Enabled: true})
	started := time.Now()
	resp, err := e.Search("golang goroutine 教程", 1, antirobot.TimeRangeNone)
	if testenv.HandleSearchError(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if resp == nil || len(resp.Results) == 0 {
		t.Fatalf("expected results, got %+v", resp)
	}
	for _, r := range resp.Results {
		if r.Title == "" || r.URL == "" {
			t.Fatalf("结果字段不完整: %+v", r)
		}
		if isSoLinkWrap(r.URL) {
			t.Fatalf("结果 URL 不应是 so.com 跳转包装: %s", r.URL)
		}
	}
	t.Logf("so360 真网 %s 返回 %d 条结果，首条: %s %s",
		time.Since(started).Round(time.Millisecond), len(resp.Results), resp.Results[0].Title, resp.Results[0].URL)
}
