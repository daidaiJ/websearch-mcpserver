package search

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func newDoubaoGlobalTestServer(t *testing.T, status int, body string) (*DoubaoSearchImpl, chan doubaoGlobalSearchRequest, chan http.Header) {
	t.Helper()
	reqCh := make(chan doubaoGlobalSearchRequest, 8)
	headerCh := make(chan http.Header, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req doubaoGlobalSearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		reqCh <- req
		headerCh <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	pool, _ := NewKeyPool([]string{"doubao-test-key"})
	engine := NewDoubaoSearch(pool, DoubaoOptions{Version: doubaoVersionGlobal})
	engine.globalEndpoint = srv.URL
	return engine, reqCh, headerCh
}

func newDoubaoCustomTestServer(t *testing.T, status int, body string) (*DoubaoSearchImpl, chan doubaoCustomSearchRequest, chan http.Header) {
	t.Helper()
	reqCh := make(chan doubaoCustomSearchRequest, 8)
	headerCh := make(chan http.Header, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req doubaoCustomSearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		reqCh <- req
		headerCh <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	pool, _ := NewKeyPool([]string{"doubao-test-key"})
	engine := NewDoubaoSearch(pool, DoubaoOptions{Version: doubaoVersionCustom})
	engine.customEndpoint = srv.URL
	return engine, reqCh, headerCh
}

const doubaoGlobalOKResp = `{
  "ResponseMetadata": {"RequestId": "rid-global"},
  "Result": {
    "TotalDocCount": 20,
    "Documents": [
      {
        "Rank": 0,
        "Url": " https://www.volcengine.com/docs/87772/2548026 ",
        "Title": "Doubao Global",
        "Snippet": [
          {"Type": "text", "Text": "Global text one"},
          {"Type": "image", "Image": {"ImageUrl": "https://example.com/a.jpg", "Alt": "image alt"}},
          {"Type": "text", "Text": "Global text two"}
        ],
        "DocumentInfo": {
          "ContentCharCount": 100,
          "ContentTokenCount": 50,
          "Filetype": "webpage",
          "PublishTime": "2026-09-11T10:00:00+08:00"
        },
        "HostInfo": {"Hostname": "Volcengine", "AuthorityLevel": "very_high"}
      }
    ],
    "ErrorCode": 0,
    "ErrorMsg": ""
  }
}`

const doubaoCustomOKResp = `{
  "ResponseMetadata": {},
  "Result": {
    "WebResults": [
      {
        "Id": "r1",
        "SortId": 1,
        "Title": "Doubao Custom",
        "SiteName": "Volcengine",
        "Url": " https://www.volcengine.com/docs/87772/2272953 ",
        "Snippet": "short",
        "Summary": "summary",
        "Content": "full content",
        "PublishTime": "2026-09-11T10:00:00+08:00",
        "RankScore": 0.91
      }
    ]
  }
}`

func TestDoubao_Global_SearchRaw_HappyPath(t *testing.T) {
	engine, reqCh, headerCh := newDoubaoGlobalTestServer(t, http.StatusOK, doubaoGlobalOKResp)
	engine.maxSnippetLength = 1000
	engine.icpHostOnly = true

	results, err := engine.SearchRaw("doubao global")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	req := <-reqCh
	if req.Query != "doubao global" || req.SearchType != "web" || req.DocCount != 10 {
		t.Fatalf("unexpected request: %+v", req)
	}
	if req.MaxSnippetLength != 1000 || req.MaxImageCountPerDoc != 0 {
		t.Fatalf("unexpected snippet/image limits: %+v", req)
	}
	if req.Filter == nil || !req.Filter.ICPHostOnly {
		t.Fatalf("expected IcpHostOnly filter, got %+v", req.Filter)
	}

	header := <-headerCh
	if got := header.Get("Authorization"); got != "Bearer doubao-test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if results[0].Url != "https://www.volcengine.com/docs/87772/2548026" {
		t.Errorf("expected trimmed URL, got %q", results[0].Url)
	}
	if results[0].Content != "Global text one\nGlobal text two" {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
	if results[0].PublishDate != "2026-09-11T10:00:00+08:00" {
		t.Errorf("unexpected publish date: %q", results[0].PublishDate)
	}
	if results[0].Engine != "doubao_global" {
		t.Errorf("expected engine doubao_global, got %q", results[0].Engine)
	}
}

func TestDoubao_Custom_SearchRaw_HappyPath(t *testing.T) {
	engine, reqCh, headerCh := newDoubaoCustomTestServer(t, http.StatusOK, doubaoCustomOKResp)

	results, err := engine.SearchRaw("doubao custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	req := <-reqCh
	if req.Query != "doubao custom" || req.SearchType != "web" || req.Count != 10 {
		t.Fatalf("unexpected request: %+v", req)
	}
	if req.Filter == nil || !req.Filter.NeedUrl || req.Filter.NeedContent {
		t.Fatalf("unexpected filter: %+v", req.Filter)
	}

	header := <-headerCh
	if got := header.Get("Authorization"); got != "Bearer doubao-test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if results[0].Url != "https://www.volcengine.com/docs/87772/2272953" {
		t.Errorf("expected trimmed URL, got %q", results[0].Url)
	}
	if results[0].Content != "full content" || results[0].Score != 0.91 {
		t.Errorf("unexpected result: %+v", results[0])
	}
	if results[0].Engine != "doubao_custom" {
		t.Errorf("expected engine doubao_custom, got %q", results[0].Engine)
	}
}

func TestDoubao_Global_AdvancedOptions(t *testing.T) {
	engine, reqCh, _ := newDoubaoGlobalTestServer(t, http.StatusOK, doubaoGlobalOKResp)
	engine.numResults = 50
	engine.maxSnippetLength = 9999
	engine.maxImageCountPerDoc = 99

	if _, err := engine.SearchRaw("current news"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := <-reqCh
	if req.DocCount != 20 {
		t.Errorf("DocCount = %d, want capped 20", req.DocCount)
	}
	if req.MaxSnippetLength != 3000 {
		t.Errorf("MaxSnippetLength = %d, want capped 3000", req.MaxSnippetLength)
	}
	if req.MaxImageCountPerDoc != 10 {
		t.Errorf("MaxImageCountPerDoc = %d, want capped 10", req.MaxImageCountPerDoc)
	}
}

func TestDoubao_Custom_AdvancedOptions(t *testing.T) {
	engine, reqCh, _ := newDoubaoCustomTestServer(t, http.StatusOK, doubaoCustomOKResp)
	engine.timeRange = "OneWeek"
	engine.authLevel = 1
	engine.queryRewrite = true
	engine.needContent = true

	if _, err := engine.SearchRaw("current news"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := <-reqCh
	if req.TimeRange != "OneWeek" {
		t.Errorf("TimeRange = %q", req.TimeRange)
	}
	if req.Filter == nil || req.Filter.AuthInfoLevel != 1 || !req.Filter.NeedContent {
		t.Errorf("unexpected filter: %+v", req.Filter)
	}
	if req.QueryControl == nil || !req.QueryControl.QueryRewrite {
		t.Errorf("expected QueryRewrite=true, got %+v", req.QueryControl)
	}
}

func TestDoubao_Global_KeyError(t *testing.T) {
	engine, _, _ := newDoubaoGlobalTestServer(t, http.StatusOK, `{"ResponseMetadata":{"Error":{"Code":"700901","Message":"invalid key"}},"Result":null}`)

	_, err := engine.SearchRaw("q")
	var keyErr *KeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("expected KeyError, got %v", err)
	}
	if keyErr.Key != "doubao-test-key" {
		t.Errorf("expected cooled key, got %q", keyErr.Key)
	}
}

func TestDoubao_Custom_StatusError_KeyError(t *testing.T) {
	engine, _, _ := newDoubaoCustomTestServer(t, http.StatusUnauthorized, `{"ResponseMetadata":{"Error":{"Code":"AccessDenied","Message":"invalid key"}}}`)

	_, err := engine.SearchRaw("q")
	var keyErr *KeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("expected KeyError, got %v", err)
	}
}

func TestDoubao_Global_EmptyResults(t *testing.T) {
	engine, _, _ := newDoubaoGlobalTestServer(t, http.StatusOK, `{"Result":{"TotalDocCount":0,"Documents":[],"ErrorCode":0,"ErrorMsg":""}}`)

	_, err := engine.SearchRaw("q")
	if err == nil {
		t.Fatal("expected error for empty results")
	}
	var keyErr *KeyError
	if errors.As(err, &keyErr) {
		t.Errorf("empty results should not be a KeyError, got %v", err)
	}
}

func TestDoubao_Both_MergesAndDeduplicates(t *testing.T) {
	global, globalReqCh, _ := newDoubaoGlobalTestServer(t, http.StatusOK, doubaoGlobalOKResp)
	custom, customReqCh, _ := newDoubaoCustomTestServer(t, http.StatusOK, `{
	  "Result": {
	    "WebResults": [
	      {
	        "Title": "Doubao Custom duplicate",
	        "Url": "https://www.volcengine.com/docs/87772/2548026",
	        "Snippet": "short duplicate",
	        "Content": "custom duplicate content"
	      },
	      {
	        "Title": "Custom only",
	        "Url": "https://example.com/custom",
	        "Snippet": "custom only"
	      }
	    ]
	  }
	}`)
	global.version = doubaoVersionBoth
	global.customEndpoint = custom.customEndpoint

	results, err := global.SearchRaw("q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 deduplicated results, got %d: %+v", len(results), results)
	}
	if results[0].Url != "https://www.volcengine.com/docs/87772/2548026" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[0].Content != "Global text one\nGlobal text two" {
		t.Errorf("expected longer global content to win, got %q", results[0].Content)
	}
	if results[1].Engine != "doubao_custom" {
		t.Errorf("expected custom engine for unique result, got %q", results[1].Engine)
	}
	<-globalReqCh
	<-customReqCh
}

func TestDoubao_Defaults(t *testing.T) {
	pool, _ := NewKeyPool([]string{"k"})
	engine := NewDoubaoSearch(pool, DoubaoOptions{})
	if engine.version != doubaoVersionGlobal {
		t.Errorf("default version = %q, want global", engine.version)
	}
	if engine.numResults != 10 {
		t.Errorf("default numResults = %d, want 10", engine.numResults)
	}
}

func TestDoubao_Name(t *testing.T) {
	pool, _ := NewKeyPool([]string{"k"})
	if engine := NewDoubaoSearch(pool, DoubaoOptions{}); engine.Name() != "doubao" {
		t.Errorf("Name() = %q, want doubao", engine.Name())
	}
}

func TestDoubaoSearchImpl_Integration(t *testing.T) {
	if testing.Short() || os.Getenv("DOUBAO_SEARCH_API_KEY") == "" {
		t.Skip("跳过集成测试: -short 或未配置 DOUBAO_SEARCH_API_KEY")
	}
	pool, err := NewKeyPool([]string{os.Getenv("DOUBAO_SEARCH_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewDoubaoSearch(pool, DoubaoOptions{Version: os.Getenv("DOUBAO_SEARCH_VERSION"), NumResults: 5})
	results, err := engine.SearchRaw("火山引擎 豆包搜索")
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
}
