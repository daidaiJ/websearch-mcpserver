package search

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newAnysearchTestServer 启动 mock AnySearch API，返回引擎实例与请求记录通道。
func newAnysearchTestServer(t *testing.T, status int, body string) (*AnysearchSearchImpl, chan anysearchSearchReq) {
	t.Helper()
	reqCh := make(chan anysearchSearchReq, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anysearchSearchReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		reqCh <- req
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	pool, _ := NewKeyPool([]string{"sk-test-1"})
	engine := NewAnysearchSearch(pool, 10, nil)
	engine.endpoint = srv.URL
	return engine, reqCh
}

const anysearchOKResp = `{"code":0,"message":"success","request_id":"rid-1","data":{"results":[` +
	`{"title":"Go 1.26 Release Notes","url":" https://go.dev/doc/go1.26 ","snippet":"short","content":"full content"},` +
	`{"title":"Blog post","url":"https://blog.example.com/post","snippet":"","content":"snippet fallback content"}` +
	`],"metadata":{"total_results":2,"search_time_ms":20}}}`

func TestAnysearch_SearchRaw_HappyPath(t *testing.T) {
	engine, reqCh := newAnysearchTestServer(t, http.StatusOK, anysearchOKResp)

	results, err := engine.SearchRaw("golang release")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 请求体与认证头
	req := <-reqCh
	if req.Query != "golang release" {
		t.Errorf("expected query 'golang release', got %q", req.Query)
	}
	if req.MaxResults != 10 {
		t.Errorf("expected max_results 10, got %d", req.MaxResults)
	}

	// 结果映射：URL trim、content 回落 snippet
	if results[0].Url != "https://go.dev/doc/go1.26" {
		t.Errorf("expected trimmed url, got %q", results[0].Url)
	}
	if results[0].Content != "full content" {
		t.Errorf("expected full content, got %q", results[0].Content)
	}
	if results[1].Content != "snippet fallback content" {
		t.Errorf("expected snippet fallback, got %q", results[1].Content)
	}
	for _, r := range results {
		if r.Engine != "anysearch" {
			t.Errorf("expected engine 'anysearch', got %s", r.Engine)
		}
	}
}

func TestAnysearch_SearchRaw_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anysearchOKResp))
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	pool, _ := NewKeyPool([]string{"sk-test-1"})
	engine := NewAnysearchSearch(pool, 10, nil)
	engine.endpoint = srv.URL
	if _, err := engine.SearchRaw("q"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer sk-test-1" {
		t.Errorf("expected 'Bearer sk-test-1', got %q", gotAuth)
	}
}

// TestAnysearch_SearchRaw_StatusError_KeyError 401/403 等状态码错误应返回 KeyError（精确冷却该 Key）。
func TestAnysearch_SearchRaw_StatusError_KeyError(t *testing.T) {
	engine, _ := newAnysearchTestServer(t, http.StatusUnauthorized, `{"code":-1,"message":"invalid key"}`)

	_, err := engine.SearchRaw("q")
	var ke *KeyError
	if !errors.As(err, &ke) {
		t.Fatalf("expected KeyError, got %v", err)
	}
	if ke.Key != "sk-test-1" {
		t.Errorf("expected cooled key sk-test-1, got %s", ke.Key)
	}
}

// TestAnysearch_SearchRaw_CodeNotZero envelope code=-1 应返回 KeyError。
func TestAnysearch_SearchRaw_CodeNotZero(t *testing.T) {
	engine, _ := newAnysearchTestServer(t, http.StatusOK, `{"code":-1,"message":"upstream error","request_id":"rid-2"}`)

	_, err := engine.SearchRaw("q")
	var ke *KeyError
	if !errors.As(err, &ke) {
		t.Fatalf("expected KeyError, got %v", err)
	}
}

// TestAnysearch_SearchRaw_EmptyResults 非空 key 但空结果属于业务空，不应冷却 Key。
func TestAnysearch_SearchRaw_EmptyResults(t *testing.T) {
	engine, _ := newAnysearchTestServer(t, http.StatusOK, `{"code":0,"message":"success","data":{"results":[]}}`)

	_, err := engine.SearchRaw("q")
	if err == nil {
		t.Fatal("expected error for empty results")
	}
	var ke *KeyError
	if errors.As(err, &ke) {
		t.Errorf("empty results should not be a KeyError, got %v", err)
	}
}

// TestAnysearch_BlacklistLocal API 不支持 exclude_domains，黑名单在本地过滤。
func TestAnysearch_BlacklistLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"results":[` +
			`{"title":"blocked site","url":"https://baijiahao.baidu.com/s?id=1","content":"c"},` +
			`{"title":"blocked subdomain","url":"https://news.csdn.net/a","content":"c"},` +
			`{"title":"good","url":"https://go.dev/doc","content":"c"}]}}`))
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	pool, _ := NewKeyPool([]string{"sk-test-1"})
	engine := NewAnysearchSearch(pool, 10, []string{"baijiahao.baidu.com", "csdn.net"})
	engine.endpoint = srv.URL

	results, err := engine.SearchRaw("q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Url != "https://go.dev/doc" {
		t.Errorf("expected only go.dev result after blacklist filtering, got %v", results)
	}
}

// TestAnysearch_NumResultsDefault numResults <= 0 时构造函数内默认 10。
func TestAnysearch_NumResultsDefault(t *testing.T) {
	pool, _ := NewKeyPool([]string{"sk-test-1"})
	if e := NewAnysearchSearch(pool, 0, nil); e.numResults != 10 {
		t.Errorf("expected default numResults 10, got %d", e.numResults)
	}
	if e := NewAnysearchSearch(pool, -5, nil); e.numResults != 10 {
		t.Errorf("expected default numResults 10 for negative input, got %d", e.numResults)
	}
}

func TestAnysearch_Name(t *testing.T) {
	engine, _ := newAnysearchTestServer(t, http.StatusOK, anysearchOKResp)
	if engine.Name() != "anysearch" {
		t.Errorf("expected anysearch, got %s", engine.Name())
	}
}

// ── 集成测试（从 config.test.yaml 加载 API Key） ──

func TestAnysearchSearchImpl_SearchRaw_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadAnysearchAPIKey(t)

	anysearch := NewAnysearchSearch(newTestKeyPool(t, apiKey), 10, []string{"csdn.net"})
	results, err := anysearch.SearchRaw("Go programming language")
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	for i, r := range results {
		t.Logf("[%d] %s - %s", i+1, r.Title, r.Url)
		if r.Title == "" {
			t.Error("result title should not be empty")
		}
		if r.Url == "" {
			t.Error("result url should not be empty")
		}
		if r.Engine != "anysearch" {
			t.Errorf("expected engine 'anysearch', got %s", r.Engine)
		}
	}
}

// ── isBlockedHost ─────────────────────────────────────────────────────────

func TestAnysearch_IsBlockedHost(t *testing.T) {
	blocked := []string{"baijiahao.baidu.com", "csdn.net", ""}
	tests := []struct {
		rawURL string
		want   bool
	}{
		{"https://baijiahao.baidu.com/s?id=1", true},         // 精确
		{"https://news.csdn.net/a", true},                    // 子域后缀
		{"https://BLOG.CSDN.NET/a", true},                    // host 大小写不敏感
		{"https://go.dev/doc", false},                        // 未命中
		{"https://evil.com/??u=https://csdn.net", false},     // 仅出现在 query 中，host 未命中
		{"https://csdn.net.evil.com/a", false},               // 后缀相似但非子域
		{"", false},                                          // 空URL
	}
	for _, tt := range tests {
		if got := isBlockedHost(tt.rawURL, blocked); got != tt.want {
			t.Errorf("isBlockedHost(%q) = %v, want %v", tt.rawURL, got, tt.want)
		}
	}
	// 空/nil 黑名单不过滤
	if isBlockedHost("https://csdn.net", nil) || isBlockedHost("https://csdn.net", []string{}) {
		t.Error("empty blacklist should not block")
	}
}
