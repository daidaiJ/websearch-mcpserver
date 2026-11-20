package sogou

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"websearch/internal/testenv"
	"websearch/pkg/antirobot"
)

const sogouFixtureHTML = `<!DOCTYPE html><html><body>
<div class="rb">
  <h3 class="pt"><a href="/link?url=abc" data-url="https://go.dev/doc/go1.26">Go 1.26 <em>release</em> notes</a></h3>
  <div class="ft">Go 1.26 已发布，包含 release 更新。</div>
  <cite>2026-11-20</cite>
</div>
<div class="vrwrap">
  <h3 class="vr-title"><a href="https://blog.example.com/post">Blog post</a></h3>
  <div class="attribute-centent">Blog summary text.</div>
</div>
<div class="vrwrap">
  <div class="special-wrap">special card</div>
  <h3 class="vr-title"><a href="https://special.example.com/">Special</a></h3>
</div>
<div class="vrwrap">
  <h3 class="vr-title"><a href="/link?url=nodata">Wrapped without data-url</a></h3>
</div>
</body></html>`

func newTestEngine(blocked []string) antirobot.Engine {
	return NewSogou(SogouOpts{Enabled: true, Blocked: blocked})
}

func TestSogou_ParseResults(t *testing.T) {
	e := newTestEngine(nil).(*sogouEngine)
	results := e.parseResults(sogouFixtureHTML)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.URL != "https://go.dev/doc/go1.26" {
		t.Errorf("data-url 应优先于 /link 包装, got %q", r.URL)
	}
	if r.Title != "Go 1.26 release notes" {
		t.Errorf("标题应去除高亮标签, got %q", r.Title)
	}
	if !strings.Contains(r.Content, "release 更新") {
		t.Errorf("摘要解析不符: %q", r.Content)
	}
	if r.PublishedAt != "2026-11-20" || r.DateSource != antirobot.DateSourceStructured {
		t.Errorf("日期解析不符: %q/%q", r.PublishedAt, r.DateSource)
	}
	if r.Engine != "sogou" || r.Type != antirobot.ResultWeb {
		t.Errorf("engine/type 标注不符: %+v", r)
	}
	if results[1].URL != "https://blog.example.com/post" {
		t.Errorf("直链条目应保留, got %q", results[1].URL)
	}
}

func TestSogou_BuildURL(t *testing.T) {
	e := newTestEngine(nil).(*sogouEngine)

	u := e.buildURL("golang 教程", 1, antirobot.TimeRangeNone)
	if !strings.HasPrefix(u, "https://www.sogou.com/web?") {
		t.Fatalf("base url mismatch: %s", u)
	}
	if !strings.Contains(u, "query=golang+%E6%95%99%E7%A8%8B") && !strings.Contains(u, "query=golang+教") {
		t.Errorf("query 应编码进 query 参数: %s", u)
	}
	if !strings.Contains(u, "page=1") {
		t.Errorf("应携带 page=1: %s", u)
	}
	if strings.Contains(u, "s_from=") || strings.Contains(u, "tsn=") {
		t.Errorf("默认不应携带时间范围参数: %s", u)
	}

	u2 := e.buildURL("golang", 3, antirobot.TimeRangeWeek)
	if !strings.Contains(u2, "page=3") || !strings.Contains(u2, "s_from=inttime_week") || !strings.Contains(u2, "tsn=1") {
		t.Errorf("时间范围/分页参数不符: %s", u2)
	}
}

func TestSogou_BlockedDetection(t *testing.T) {
	u, _ := url.Parse("https://www.sogou.com/antispider/?from=x")
	resp := &http.Response{Request: &http.Request{URL: u}}
	if !isSogouBlocked(resp, "normal body") {
		t.Error("antispider 跳转应判为拦截")
	}
	if !isSogouBlocked(nil, `<div id="seccode"></div>`) {
		t.Error("seccode 页面应判为拦截")
	}
	if isSogouBlocked(nil, sogouFixtureHTML) {
		t.Error("正常结果页不应误判")
	}
}

func TestSogou_SearchContext(t *testing.T) {
	origBase, origJitter := sogouBaseDelay, sogouJitter
	sogouBaseDelay, sogouJitter = time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		sogouBaseDelay, sogouJitter = origBase, origJitter
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, sogouFixtureHTML)
	}))
	defer srv.Close()

	e := NewSogou(SogouOpts{Enabled: true, PerSec: 1000, PerMin: 10000}).(*sogouEngine)
	e.endpoint = srv.URL
	e.client = srv.Client()

	resp, err := e.Search("golang", 1, antirobot.TimeRangeNone)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
}

// TestSogou_SearchIntegration 真网集成：解析契约对真实结果页生效。
// 网络场景自动跳过；命中 antispider 归一为场景错误。
func TestSogou_SearchIntegration(t *testing.T) {
	testenv.Require(t, testenv.Sogou)
	e := NewSogou(SogouOpts{Enabled: true, PerSec: 1000, PerMin: 100000})
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
		if isSogouLinkWrap(r.URL) {
			t.Fatalf("结果 URL 不应是 sogou /link 包装: %s", r.URL)
		}
	}
	t.Logf("sogou 真网返回 %d 条结果，首条: %s %s", len(resp.Results), resp.Results[0].Title, resp.Results[0].URL)
}
