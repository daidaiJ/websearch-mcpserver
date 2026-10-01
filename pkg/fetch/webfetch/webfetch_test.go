package webfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"websearch/internal/testenv"
	"websearch/pkg/config"
	"websearch/pkg/fetch/mineru"

	webfetch "github.com/daidaiJ/go-webfetch"
)

func newTestFetcher(t *testing.T) *Fetcher {
	t.Helper()
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		Enabled:        true,
		FileTTL:        1,
		MaxInlineLines: 100,
	}, config.PDFParserConfig{}, config.ProxyConfig{}.GetProxyEndpoint())
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	return fetcher
}

func TestFetchWebPage(t *testing.T) {
	testenv.Require(t, testenv.WmySkxz)
	fetcher := newTestFetcher(t)
	defer fetcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := fetcher.Fetch(ctx, "https://wmyskxz.cn/weekly/177/")
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("Fetch failed: %v", err)
	}

	if result.Title == "" {
		t.Error("expected non-empty title")
	}
	if result.Mode == "" {
		t.Error("expected non-empty mode")
	}
	if result.Mode == "inline" && result.Markdown == "" {
		t.Error("inline mode but markdown is empty")
	}
	if result.Mode == "saved_to_file" && result.FilePath == "" {
		t.Error("saved_to_file mode but file path is empty")
	}

	t.Logf("Title: %s", result.Title)
	t.Logf("Mode: %s", result.Mode)
	if result.Mode == "inline" {
		t.Logf("Markdown length: %d chars", len(result.Markdown))
	} else {
		t.Logf("File: %s (%d lines, %d chars)", result.FilePath, result.TotalLines, result.TotalChars)
	}
}

func TestFetchPDF(t *testing.T) {
	// 需要本地 PDF 文件时通过环境变量传入，避免硬编码路径
	pdfPath := os.Getenv("TEST_PDF_PATH")
	if pdfPath == "" {
		t.Skip("TEST_PDF_PATH not set, skipping PDF test")
	}
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skipf("PDF file not found: %s", pdfPath)
	}

	fetcher := newTestFetcher(t)
	defer fetcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	absPath, _ := filepath.Abs(pdfPath)
	// Windows file:// URL 需要三斜杠 + 正斜杠
	fileURL := "file:///" + strings.ReplaceAll(absPath, `\`, "/")

	result, err := fetcher.Fetch(ctx, fileURL)
	if err != nil {
		t.Fatalf("Fetch PDF failed: %v", err)
	}

	if result.Title == "" {
		t.Error("expected non-empty title")
	}
	if result.Mode == "inline" && result.Markdown == "" {
		t.Error("inline mode but markdown is empty")
	}

	t.Logf("Title: %s", result.Title)
	t.Logf("Mode: %s", result.Mode)
	if result.Mode == "inline" {
		t.Logf("Markdown length: %d chars", len(result.Markdown))
	} else {
		t.Logf("File: %s (%d lines, %d chars)", result.FilePath, result.TotalLines, result.TotalChars)
	}
}

func TestClassifyErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected string
	}{
		{"nil error", nil, ""},
	}
	for _, tt := range tests {
		if tt.input != nil {
			t.Run(tt.name, func(t *testing.T) {
				got := classifyError(tt.input)
				if !strings.Contains(got, tt.expected) {
					t.Errorf("classifyError(%v) = %q, want contains %q", tt.input, got, tt.expected)
				}
			})
		}
	}
}

func TestNeedsOCRFallback(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no text extracted", fmt.Errorf("PDF 解析失败: no text extracted — possible causes: scanned"), true},
		{"scanned mention", fmt.Errorf("scanned/image-based PDF (no text layer)"), true},
		{"file not found", fmt.Errorf("PDF 解析失败: file not found: /tmp/x.pdf"), false},
		{"other error", fmt.Errorf("PDF 解析失败: open pdf: corrupt"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsOCRFallback(tt.err); got != tt.want {
				t.Errorf("needsOCRFallback(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewFromConfig_MinerUOCR(t *testing.T) {
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		Enabled: true,
	}, config.PDFParserConfig{
		Enabled:   true,
		MinerUOcr: true,
	}, "")
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	defer fetcher.Close()

	if fetcher.mineru == nil {
		t.Error("expected mineru client when mineru_ocr=true")
	}
	if !fetcher.mineruOCR {
		t.Error("expected mineruOCR=true")
	}
}

func TestNewFromConfig_NoMinerUWithoutOCROrToken(t *testing.T) {
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		Enabled: true,
	}, config.PDFParserConfig{
		Enabled: true,
	}, "")
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	defer fetcher.Close()

	if fetcher.mineru != nil {
		t.Error("expected no mineru client when neither token nor ocr")
	}
	if fetcher.mineruOCR {
		t.Error("expected mineruOCR=false")
	}
}

func TestParseLocalPDF_OCRHintWithoutConfig(t *testing.T) {
	fetcher := newTestFetcher(t)
	defer fetcher.Close()

	_, err := fetcher.parseLocalPDF(context.Background(), filepath.Join(os.TempDir(), "nonexistent-webfetch-ocr-test.pdf"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if strings.Contains(err.Error(), "mineru_ocr") {
		t.Errorf("file-not-found should not suggest mineru_ocr: %v", err)
	}
}

func TestNewFromConfigDefaults(t *testing.T) {
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		Enabled: true,
	}, config.PDFParserConfig{}, config.ProxyConfig{}.GetProxyEndpoint())
	if err != nil {
		t.Fatalf("NewFromConfig with defaults failed: %v", err)
	}
	defer fetcher.Close()
}

// ── MinerU 仅用于 PDF URL（T03）────────────────────────────────────────────

// spyMineru 记录调用次数的 MinerU 客户端替身。
type spyMineru struct {
	hasToken      bool
	parseURLCalls int
	parseURLErr   error
	pages         []int
	maxPages      int
}

func (s *spyMineru) HasToken() bool { return s.hasToken }
func (s *spyMineru) PageLimit() int { return 600 }
func (s *spyMineru) ParseURL(ctx context.Context, fileURL string) (string, error) {
	s.parseURLCalls++
	return "# MinerU", nil
}
func (s *spyMineru) ParseURLWithPages(ctx context.Context, fileURL string, pages []int, maxPages int) (string, error) {
	s.parseURLCalls++
	s.pages, s.maxPages = pages, maxPages
	return "# MinerU", s.parseURLErr
}
func (s *spyMineru) ParseStandardFile(ctx context.Context, filePath string) (string, error) {
	return "# MinerU upload", nil
}
func (s *spyMineru) ParseFileWithPages(ctx context.Context, filePath string, pages []int, maxPages int) (string, error) {
	return "", nil
}

// stubEngine 返回固定结果的抓取引擎替身。
type stubEngine struct{}

func (e *stubEngine) Fetch(ctx context.Context, rawURL string) (*webfetch.FetchResult, error) {
	return &webfetch.FetchResult{Title: "stub", Mode: "inline", Markdown: "stub"}, nil
}
func (e *stubEngine) FetchWithOpts(ctx context.Context, rawURL string, opts webfetch.FetchOptions) (*webfetch.FetchResult, error) {
	return &webfetch.FetchResult{Title: "stub", Mode: "inline", Markdown: "stub"}, nil
}
func (e *stubEngine) ParsePDFFile(ctx context.Context, filePath string, opts ...webfetch.PDFOption) (*webfetch.PDFResult, error) {
	return &webfetch.PDFResult{Title: "stub", Mode: "inline", Markdown: "stub"}, nil
}
func (e *stubEngine) Close() error { return nil }

// TestIdleForCleanup 验证空闲判定：进行中抓取或近期有活动时不可清理，
// 零值 lastActivity（新启动无流量）视为空闲。
func TestIdleForCleanup(t *testing.T) {
	f := &Fetcher{}
	if !f.idleForCleanup() {
		t.Fatal("zero-value Fetcher (no activity) should be idle")
	}
	f.beginActivity()
	if f.idleForCleanup() {
		t.Fatal("in-flight fetch must block cleanup")
	}
	f.endActivity()
	if f.idleForCleanup() {
		t.Fatal("activity just ended, threshold not elapsed yet")
	}
	f.lastActivity.Store(time.Now().Add(-2 * fileCleanupIdleThreshold).UnixNano())
	if !f.idleForCleanup() {
		t.Fatal("idle beyond threshold should allow cleanup")
	}
}

func TestFetch_MineruOnlyForPDFURL(t *testing.T) {
	tests := []struct {
		name            string
		rawURL          string
		hasToken        bool
		mineruRemotePDF bool
		wantParse       int
	}{
		{"html url with token", "https://example.com/a.html", true, true, 0},
		{"pdf url with token", "https://cdn.example.com/x.PDF?download=1", true, true, 1},
		{"lowercase pdf", "https://example.com/doc.pdf", true, true, 1},
		{"pdf with fragment", "https://example.com/doc.pdf#page=2", true, true, 1},
		{"non-pdf extension", "https://example.com/a.pdfx", true, true, 0},
		{"no token still skips", "https://example.com/doc.pdf", false, true, 0},
		{"remote pdf disabled by config", "https://example.com/doc.pdf", true, false, 0},
		{"invalid url", "://bad", true, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyMineru{hasToken: tt.hasToken}
			f := &Fetcher{engine: &stubEngine{}, mineru: spy, mineruRemotePDF: tt.mineruRemotePDF}
			_, err := f.Fetch(context.Background(), tt.rawURL)
			if err != nil {
				t.Fatalf("Fetch failed: %v", err)
			}
			if spy.parseURLCalls != tt.wantParse {
				t.Errorf("ParseURL calls = %d, want %d", spy.parseURLCalls, tt.wantParse)
			}
		})
	}
}

func TestFetchPDFWithPages_PassesMinerUPages(t *testing.T) {
	spy := &spyMineru{hasToken: true}
	f := &Fetcher{engine: &stubEngine{}, mineru: spy, mineruRemotePDF: true}
	result, err := f.FetchPDFWithPages(context.Background(), "https://example.com/doc.pdf", []int{2, 4}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if spy.parseURLCalls != 1 || len(spy.pages) != 2 || spy.pages[0] != 2 || spy.pages[1] != 4 || spy.maxPages != 20 {
		t.Fatalf("MinerU page request = %v, max=%d, calls=%d", spy.pages, spy.maxPages, spy.parseURLCalls)
	}
	if result.ParseEngine != "mineru-remote" || strings.Contains(result.Preamble, "不支持按页") {
		t.Fatalf("unexpected MinerU result: %+v", result)
	}
}

func TestFetchPDFWithPages_ReportsMinerUPageLimitForLocalCrop(t *testing.T) {
	for _, want := range []error{mineru.ErrPageLimit, mineru.ErrRemoteURLRejected} {
		spy := &spyMineru{hasToken: true, parseURLErr: want}
		f := &Fetcher{engine: &stubEngine{}, mineru: spy, mineruRemotePDF: true}
		_, err := f.FetchPDFWithPages(context.Background(), "https://example.com/doc.pdf", []int{201}, 20)
		if !errors.Is(err, want) {
			t.Fatalf("expected retryable MinerU error %v, got %v", want, err)
		}
	}
}

func TestMinerUPageNoteForLongAgentPDF(t *testing.T) {
	note := mineruPageNote([]int{21, 22}, 20, true, 20)
	if !strings.Contains(note, "继续用 pages") || strings.Contains(note, "须先拆分") {
		t.Fatalf("misleading Agent page guidance: %s", note)
	}
}

func TestIsPDFURL(t *testing.T) {
	tests := []struct {
		rawURL string
		want   bool
	}{
		{"https://example.com/a.pdf", true},
		{"https://example.com/a.PDF", true},
		{"https://example.com/a.Pdf?x=1", true},
		{"https://example.com/a.pdf#frag", true},
		{"https://example.com/a.html", false},
		{"https://example.com/a.pdfx", false},
		{"https://example.com/", false},
		{"https://example.com", false},
		{"://bad", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isPDFURL(tt.rawURL); got != tt.want {
			t.Errorf("isPDFURL(%q) = %v, want %v", tt.rawURL, got, tt.want)
		}
	}
}

func TestFetchRuanyifengBlog(t *testing.T) {
	testenv.Require(t, testenv.Ruanyifeng)
	fetcher := newTestFetcher(t)
	defer fetcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := fetcher.Fetch(ctx, "https://www.ruanyifeng.com/blog/2026/07/weekly-issue-406.html")
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("Fetch failed: %v", err)
	}

	if result.Title == "" {
		t.Error("expected non-empty title")
	}
	t.Logf("Title: %s", result.Title)
	t.Logf("Mode: %s", result.Mode)
	if result.Mode == "inline" {
		t.Logf("Markdown length: %d chars", len(result.Markdown))
	} else {
		t.Logf("File: %s (%d lines, %d chars)", result.FilePath, result.TotalLines, result.TotalChars)
	}
}

// TestCleanExpiredFiles_StrictFilter 验证清理只删除"本程序命名模式的过期 .md"：
// 过期且匹配模式 → 删；同名但未过期 → 留；其它名字/扩展名的用户文件 → 留；
// 同名目录 → 留。
func TestCleanExpiredFiles_StrictFilter(t *testing.T) {
	dir := t.TempDir()
	f := &Fetcher{outputDir: dir, fileTTL: time.Hour}

	old := time.Now().Add(-2 * time.Hour)
	fresh := time.Now()
	files := map[string]time.Time{
		"20240101_000000_some-title_a1b2c3.md": old,   // 过期 + 匹配 → 删
		"20260913_194110_rfc-editor_e3b0c4.md": fresh, // 匹配但未过期 → 留
		"20240101_000000_notes_a1b2c3.txt":     old,   // 扩展名不符 → 留
		"notes.md":                             old,   // 用户自己的 md → 留
		"20240101_000000_bad-hash_a1b2zz.md":   old,   // hash 段非 hex → 留
		"random-20240101_000000_x_a1b2c3.md":   old,   // 前缀不符 → 留
	}
	for name, mt := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, name), mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// 同名目录 → 留
	if err := os.Mkdir(filepath.Join(dir, "20240101_000000_dir_e3b0c4.md"), 0755); err != nil {
		t.Fatal(err)
	}

	n, err := f.CleanExpiredFiles()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 file removed, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "20240101_000000_some-title_a1b2c3.md")); !os.IsNotExist(err) {
		t.Fatal("expired matching file should be removed")
	}
	for name := range files {
		if name == "20240101_000000_some-title_a1b2c3.md" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("file %s should be preserved, got %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "20240101_000000_dir_e3b0c4.md")); err != nil {
		t.Fatal("directory with matching name must be preserved")
	}
}

// TestCleanExpiredFiles_MissingDir 验证输出目录不存在时静默返回 0 而非报错。
func TestCleanExpiredFiles_MissingDir(t *testing.T) {
	f := &Fetcher{outputDir: filepath.Join(t.TempDir(), "not-created"), fileTTL: time.Hour}
	n, err := f.CleanExpiredFiles()
	if err != nil || n != 0 {
		t.Fatalf("missing dir should be (0, nil), got (%d, %v)", n, err)
	}
}
