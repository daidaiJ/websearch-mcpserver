package wikipedia

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"websearch/internal/testenv"
	"websearch/pkg/antirobot"
	"websearch/pkg/proxy"
)

const wikiFixtureJSON = `{"batchcomplete":"","continue":{"sroffset":2,"continue":"-||"},
"query":{"searchinfo":{"totalhits":18},"search":[
{"ns":0,"title":"Go (programming language)","pageid":4476807,"size":123456,"wordcount":9000,
 "snippet":"<b>Go</b> is a <span class=\"searchmatch\">programming</span> language &amp; toolchain"},
{"ns":0,"title":"Go 语言","pageid":1165348,"size":25442,"wordcount":2320,
 "snippet":"<b>Go</b>（又称 Golang）是 <span class=\"searchmatch\">编程</span>&nbsp;语言"}
]}}`

// newTestEngine 起 httptest 假 API 服务并把它指为引擎端点。
func newTestEngine(t *testing.T, body string, status int) antirobot.Engine {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "list=search") {
			t.Errorf("请求应携带 list=search 参数: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	orig := wikipediaEndpoint
	wikipediaEndpoint = srv.URL + "/w/api.php"
	t.Cleanup(func() { wikipediaEndpoint = orig })

	return NewWikipedia(WikipediaOpts{Enabled: true, Lang: "zh", ProxyResolve: func() string { return "" }})
}

func TestWikipedia_ParseResults(t *testing.T) {
	resp, err := newTestEngine(t, wikiFixtureJSON, http.StatusOK).Search("golang", 1, antirobot.TimeRangeNone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %+v", resp)
	}
	r := resp.Results[0]
	if r.Title != "Go (programming language)" {
		t.Errorf("title mismatch: %q", r.Title)
	}
	if r.URL != "https://zh.wikipedia.org/wiki/Go_%28programming_language%29" {
		t.Errorf("article url mismatch: %q", r.URL)
	}
	if strings.Contains(r.Content, "<span") || strings.Contains(r.Content, "&amp;") {
		t.Errorf("snippet 应去除 HTML 标签并解码实体: %q", r.Content)
	}
	if !strings.Contains(r.Content, "programming language") {
		t.Errorf("snippet 文本应保留: %q", r.Content)
	}
	if r.Engine != "wikipedia" || r.Type != antirobot.ResultWeb {
		t.Errorf("engine/type 标注不符: %+v", r)
	}
	// 中文标题空格转下划线 + 路径转义
	if resp.Results[1].URL != "https://zh.wikipedia.org/wiki/Go_%E8%AF%AD%E8%A8%80" {
		t.Errorf("中文条目 url mismatch: %q", resp.Results[1].URL)
	}
}

func TestWikipedia_Pagination(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	orig := wikipediaEndpoint
	wikipediaEndpoint = srv.URL + "/w/api.php"
	t.Cleanup(func() { wikipediaEndpoint = orig })

	e := NewWikipedia(WikipediaOpts{Enabled: true, Lang: "en", NumResults: 10})
	if _, err := e.Search("test", 2, antirobot.TimeRangeNone); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rawQuery, "sroffset=10") {
		t.Errorf("第 2 页应携带 sroffset=10: %s", rawQuery)
	}
	if !strings.Contains(rawQuery, "srlimit=10") {
		t.Errorf("应携带 srlimit: %s", rawQuery)
	}
}

func TestWikipedia_APIError(t *testing.T) {
	_, err := newTestEngine(t, `{"error":{"code":"badquery","info":"invalid query"}}`, http.StatusOK).Search("x", 1, antirobot.TimeRangeNone)
	if err == nil {
		t.Fatal("API error 应返回错误")
	}
	if !strings.Contains(err.Error(), "invalid query") {
		t.Fatalf("错误应带 API info: %v", err)
	}
}

func TestWikipedia_HTTPStatusError(t *testing.T) {
	_, err := newTestEngine(t, "forbidden", http.StatusForbidden).Search("x", 1, antirobot.TimeRangeNone)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("非 200 应报 HTTP 状态码错误: %v", err)
	}
}

// TestWikipedia_SearchIntegration 真网集成：国内出口需代理（与 DDG 同约束）。
func TestWikipedia_SearchIntegration(t *testing.T) {
	testenv.Require(t, testenv.Wikipedia)
	e := NewWikipedia(WikipediaOpts{Enabled: true, Lang: "zh", ProxyResolve: proxy.DetectSystemProxy})
	resp, err := e.Search("Go 语言", 1, antirobot.TimeRangeNone)
	if testenv.HandleSearchError(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results")
	}
	for _, r := range resp.Results {
		if !strings.Contains(r.URL, "wikipedia.org/wiki/") {
			t.Fatalf("结果 URL 应指向 wiki 条目: %s", r.URL)
		}
	}
	t.Logf("wikipedia 真网返回 %d 条结果，首条: %s %s", len(resp.Results), resp.Results[0].Title, resp.Results[0].URL)
}
