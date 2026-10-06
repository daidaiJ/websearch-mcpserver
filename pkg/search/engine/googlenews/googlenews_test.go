package googlenews

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"websearch/internal/testenv"
	"websearch/pkg/antirobot"
	"websearch/pkg/proxy"
)

const rssFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<title>"golang" - Google 新闻</title>
<item><title>Go 1.26 发布 - 示例日报</title>
<link>https://news.google.com/rss/articles/CBMiABC123?oc=5</link>
<guid>https://news.google.com/rss/articles/CBMiABC123?oc=5</guid>
<pubDate>Wed, 01 Apr 2026 07:00:00 GMT</pubDate>
<description>&lt;a href="https://example.com"&gt;示例日报&lt;/a&gt; &amp;nbsp; Go 1.26 正式发布，&lt;b&gt;含泛型优化&lt;/b&gt;。</description>
<source url="https://example.com">示例日报</source></item>
<item><title>无日期条目 - 另一家</title>
<link>https://news.google.com/rss/articles/CBMiXYZ789?oc=5</link>
<description>plain text</description></item>
</channel></rss>`

// shellPageFixture 文章壳页（含 data-n-a-* 属性，~600KB 壳的真实形态抽样）。
const shellPageFixture = `<html><body><c-wiz data-p="x" data-n-a-sg="SIG123" data-n-a-ts="1717020000" data-n-a-id="CBMiABC123">loading</c-wiz></body></html>`

func TestGoogleNews_ParseRSS(t *testing.T) {
	e := NewGoogleNews(GoogleNewsOpts{Enabled: true}).(*gnewsEngine)
	results := e.parseRSS([]byte(rssFixture))
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	r := results[0]
	if r.Title != "Go 1.26 发布 - 示例日报" {
		t.Errorf("title mismatch: %q", r.Title)
	}
	if r.URL != "https://news.google.com/rss/articles/CBMiABC123?oc=5" {
		t.Errorf("link mismatch: %q", r.URL)
	}
	// RSS <pubDate> 为结构化日期
	if r.PublishedAt != "2026-04-01" || r.DateSource != antirobot.DateSourceStructured {
		t.Errorf("date provenance mismatch: %q %q", r.PublishedAt, r.DateSource)
	}
	// description 去 HTML 标签 + 实体解码
	if strings.Contains(r.Content, "<b>") || strings.Contains(r.Content, "&nbsp;") || strings.Contains(r.Content, "&amp;") {
		t.Errorf("description 应去除标签并解码实体: %q", r.Content)
	}
	if !strings.Contains(r.Content, "Go 1.26 正式发布") {
		t.Errorf("正文应保留: %q", r.Content)
	}
	// 无 pubDate 的条目为 undated
	if results[1].PublishedAt != "" || results[1].DateSource != "" {
		t.Errorf("无日期条目不应带日期: %+v", results[1])
	}
}

func TestGoogleNews_BuildURL(t *testing.T) {
	e := NewGoogleNews(GoogleNewsOpts{Enabled: true, Edition: "zh-CN"}).(*gnewsEngine)
	u := e.buildURL("golang 教程", antirobot.TimeRangeNone)
	for _, want := range []string{"q=golang+", "hl=zh-CN", "gl=CN", "ceid=CN%3Azh-Hans"} {
		if !strings.Contains(u, want) {
			t.Errorf("URL 缺少 %q: %s", want, u)
		}
	}
	// freshness → when: 操作符
	u2 := e.buildURL("golang", antirobot.TimeRangeWeek)
	if !strings.Contains(u2, "q=golang+when%3A7d") && !strings.Contains(u2, "when:7d") {
		t.Errorf("week 应映射 when:7d: %s", u2)
	}
	// 未知版本回落 zh-CN
	e2 := NewGoogleNews(GoogleNewsOpts{Enabled: true, Edition: "xx-XX"}).(*gnewsEngine)
	if u3 := e2.buildURL("q", antirobot.TimeRangeNone); !strings.Contains(u3, "hl=zh-CN") {
		t.Errorf("未知版本应回落 zh-CN: %s", u3)
	}
}

func TestGoogleNews_IsGoogleNewsURL(t *testing.T) {
	cases := map[string]bool{
		"https://news.google.com/rss/articles/CBMiABC?oc=5": true,
		"https://news.google.com/articles/CBMiABC":          true,
		"https://news.google.com/read/CBMiABC":              true,
		"https://www.news.google.com/rss/articles/CBMiABC":  true,
		"https://example.com/rss/articles/CBMiABC":          false,
		"https://news.google.com/search?q=x":                false,
		"":                                                  false,
	}
	for u, want := range cases {
		if got := isGoogleNewsURL(u); got != want {
			t.Errorf("isGoogleNewsURL(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestGoogleNews_ResolveViaBatchExecute(t *testing.T) {
	var batchBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/rss/articles/"), strings.Contains(r.URL.Path, "/articles/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(shellPageFixture))
		case strings.Contains(r.URL.Path, "batchexecute"):
			batchBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`)]}'` + "\n\n" + `[[["wrb.fr","Fbv4je","[\"garturlres\",\"https://www.51cto.com/article/839571.html\",1]",null,null,null,"generic"]]]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	origExec, origPrefix := batchexecuteURL, shellPagePrefix
	batchexecuteURL = srv.URL + "/batchexecute"
	shellPagePrefix = srv.URL
	t.Cleanup(func() { batchexecuteURL, shellPagePrefix = origExec, origPrefix })

	client := srv.Client()
	ctx := context.Background()

	// 跳转 blob 的 host 校验依赖 news.google.com（httptest 是 127.0.0.1），
	// 这里直接驱动 RPC 两步：壳页属性提取 → batchexecute 回源
	shellURL := srv.URL + "/rss/articles/CBMiABC123?oc=5"
	sg, ts, id, err := fetchShellAttrs(ctx, client, shellURL)
	if err != nil {
		t.Fatalf("壳页属性提取失败: %v", err)
	}
	if sg != "SIG123" || ts != "1717020000" || id != "CBMiABC123" {
		t.Fatalf("属性提取不符: sg=%q ts=%q id=%q", sg, ts, id)
	}
	resolved, err := postBatchExecute(ctx, client, sg, ts, id)
	if err != nil {
		t.Fatalf("batchexecute 失败: %v", err)
	}
	if resolved != "https://www.51cto.com/article/839571.html" {
		t.Fatalf("应解析出发布方 URL, got %q", resolved)
	}
	if !strings.Contains(string(batchBody), "Fbv4je") || !strings.Contains(string(batchBody), "garturlreq") {
		t.Errorf("batchexecute 请求应携带 garturlreq/f.req: %s", batchBody)
	}
	// 进程内记忆写入/读取
	memoPut("https://news.google.com/rss/articles/FAKE", resolved)
	if memoGet("https://news.google.com/rss/articles/FAKE") != resolved {
		t.Fatal("解析结果应写入记忆")
	}
}

func TestGoogleNews_ResolveShellsFailuresKeepRedirect(t *testing.T) {
	// 壳页缺属性 → 解析失败返回空串，调用方保留原链接
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>no attrs</body></html>"))
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	origPrefix := shellPagePrefix
	shellPagePrefix = srv.URL
	t.Cleanup(func() { shellPagePrefix = origPrefix })

	if got := resolveGoogleNewsURL(context.Background(), srv.Client(), srv.URL+"/rss/articles/CBMiXYZ?oc=5"); got != "" {
		t.Fatalf("壳页缺属性应解析失败, got %q", got)
	}
}

// TestGoogleNews_SearchIntegration 真网集成：RSS + 发布方 URL 回源（国内出口需代理）。
func TestGoogleNews_SearchIntegration(t *testing.T) {
	testenv.Require(t, testenv.GoogleNews)
	e := NewGoogleNews(GoogleNewsOpts{Enabled: true, Edition: "zh-CN", ProxyResolve: proxy.DetectSystemProxy})
	resp, err := e.Search("人工智能", 1, antirobot.TimeRangeNone)
	if testenv.HandleSearchError(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results")
	}
	publisher := 0
	for _, r := range resp.Results {
		if !strings.Contains(r.URL, "news.google.com") {
			publisher++
		}
	}
	if publisher == 0 {
		t.Fatal("至少应有部分结果回源为发布方 URL")
	}
	t.Logf("googlenews 真网 %d 条结果（%d 条已回源），首条: %s %s",
		len(resp.Results), publisher, resp.Results[0].Title, resp.Results[0].URL)
}
