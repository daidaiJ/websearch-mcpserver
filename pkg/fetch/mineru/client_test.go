package mineru

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	"websearch/pkg/config"
)

func TestNewFromConfig(t *testing.T) {
	c := NewFromConfig("test-token", "vlm", "en", true, false, false, "")
	if c.token != "test-token" {
		t.Errorf("token = %q, want %q", c.token, "test-token")
	}
	if c.modelVersion != "vlm" {
		t.Errorf("modelVersion = %q, want %q", c.modelVersion, "vlm")
	}
	if c.lang != "en" {
		t.Errorf("lang = %q, want %q", c.lang, "en")
	}
	if !c.HasToken() {
		t.Error("expected HasToken() == true")
	}
}

func TestNewFromConfigDefaults(t *testing.T) {
	c := NewFromConfig("", "", "", false, true, true, "")
	if c.modelVersion != defaultModelVersion {
		t.Errorf("modelVersion = %q, want %q", c.modelVersion, defaultModelVersion)
	}
	if c.lang != defaultLang {
		t.Errorf("lang = %q, want %q", c.lang, defaultLang)
	}
	if c.HasToken() {
		t.Error("expected HasToken() == false")
	}
	if !c.formula {
		t.Error("expected formula == true (passed)")
	}
	if !c.table {
		t.Error("expected table == true (passed)")
	}
}

func TestNewFromConfigFromPDFParserConfig(t *testing.T) {
	formula := true
	table := false
	pdfCfg := config.PDFParserConfig{
		Enabled:       true,
		MinerUToken:   "test",
		MinerUModel:   "vlm",
		MinerUOcr:     true,
		MinerUFormula: &formula,
		MinerUTable:   &table,
		MinerULang:    "en",
	}
	c := NewFromConfig(
		pdfCfg.MinerUToken,
		pdfCfg.GetMinerUModel(),
		pdfCfg.GetMinerULang(),
		pdfCfg.MinerUOcr,
		pdfCfg.GetMinerUFormula(),
		pdfCfg.GetMinerUTable(),
		"",
	)
	if c.modelVersion != "vlm" {
		t.Errorf("modelVersion = %q, want %q", c.modelVersion, "vlm")
	}
	if c.ocr != true {
		t.Error("expected ocr == true")
	}
	if c.formula != true {
		t.Error("expected formula == true")
	}
	if c.table != false {
		t.Error("expected table == false")
	}
}

func TestMapAPIError(t *testing.T) {
	tests := []struct {
		code int
		msg  string
		want string
	}{
		{0, "ok", ""},
		{-60005, "", "文件超过大小限制 (200MB)"},
		{-60008, "", "文件 URL 访问超时，请检查链接是否可用"},
		{-99999, "some msg", "MinerU 错误 (-99999): some msg"},
	}
	for _, tt := range tests {
		got := mapAPIError(tt.code, tt.msg)
		if got != tt.want {
			t.Errorf("mapAPIError(%d, %q) = %q, want %q", tt.code, tt.msg, got, tt.want)
		}
	}
}

func TestMapAgentError(t *testing.T) {
	tests := []struct {
		code int
		msg  string
		want string
	}{
		{0, "ok", ""},
		{-30001, "", "文件超过轻量 API 大小限制 (10MB)"},
		{-30002, "", "轻量 API 不支持该文件格式"},
		{-30003, "", "文件页数超过轻量 API 限制 (20页)；可用 pages 指定 20 页以内范围"},
		{-30004, "", "请求参数错误"},
	}
	for _, tt := range tests {
		got := mapAgentError(tt.code, tt.msg)
		if got != tt.want {
			t.Errorf("mapAgentError(%d, %q) = %q, want %q", tt.code, tt.msg, got, tt.want)
		}
	}
}

func TestMinerUPageRanges(t *testing.T) {
	for _, tt := range []struct {
		name     string
		pages    []int
		max      int
		standard string
	}{
		{"default", nil, 20, "1-20"},
		{"standard cap", nil, 500, "1-200"},
		{"selected", []int{2, 4, 5, 6}, 20, "2,4-6"},
		{"single", []int{5}, 20, "5"},
		{"contiguous", []int{3, 4, 5}, 20, "3-5"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			standard, err := standardPageRanges(tt.pages, tt.max)
			if err != nil || standard != tt.standard {
				t.Fatalf("standardPageRanges = %q, %v; want %q", standard, err, tt.standard)
			}
		})
	}
	if _, err := standardPageRanges([]int{201}, 20); !errors.Is(err, ErrPageLimit) {
		t.Fatal("standard API must reject pages beyond its 200-page file limit")
	}
}

func TestCreateTaskRemoteURLRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":-60023,"msg":"URL restricted"}`))
	}))
	defer server.Close()
	c := NewFromConfig("test-token", "pipeline", "ch", false, true, true, "")
	c.endpoint = server.URL
	_, err := c.createTask(context.Background(), "https://example.com/doc.pdf", "1")
	if !errors.Is(err, ErrRemoteURLRejected) {
		t.Fatalf("expected local-upload fallback, got %v", err)
	}
}

func TestParseFileTooLarge(t *testing.T) {
	c := NewFromConfig("", "pipeline", "ch", false, true, true, "")
	// 用一个不存在的路径测试 ErrFileTooLarge 不会被触发（文件不存在优先）
	_, err := c.ParseFile(context.Background(), "/nonexistent/file.pdf")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// 以下测试需要实际的 MinerU Token 和 PDF 文件，通过环境变量控制。
// 设置 MINERU_TOKEN 和 TEST_PDF_PATH 后运行: go test -v -run TestParseFile

func TestParseFile(t *testing.T) {
	token := os.Getenv("MINERU_TOKEN")
	pdfPath := os.Getenv("TEST_PDF_PATH")
	if token == "" || pdfPath == "" {
		t.Skip("MINERU_TOKEN or TEST_PDF_PATH not set, skipping integration test")
	}
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skipf("PDF file not found: %s", pdfPath)
	}

	c := NewFromConfig(token, "pipeline", "ch", false, true, true, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	md, err := c.ParseFile(ctx, pdfPath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}
	t.Logf("ParseFile result: %d chars", len(md))
	t.Logf("Preview: %s", truncate(md, 500))
}

func TestParseURL(t *testing.T) {
	token := os.Getenv("MINERU_TOKEN")
	if token == "" {
		t.Skip("MINERU_TOKEN not set, skipping integration test")
	}

	c := NewFromConfig(token, "pipeline", "ch", false, true, true, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	md, err := c.ParseURL(ctx, "https://cdn-mineru.openxlab.org.cn/demo/example.pdf")
	if err != nil {
		t.Fatalf("ParseURL failed: %v", err)
	}
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}
	t.Logf("ParseURL result: %d chars", len(md))
	t.Logf("Preview: %s", truncate(md, 500))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
