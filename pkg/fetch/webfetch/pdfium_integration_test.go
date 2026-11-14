package webfetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/fetch/mineru"
	"websearch/pkg/fetch/pdfium"
)

type stubPDFium struct {
	result   *pdfium.Result
	err      error
	calls    int
	gotPages []int
	gotMax   int
}

func (s *stubPDFium) Extract(_ context.Context, _ string, pages []int, maxPages int) (*pdfium.Result, error) {
	s.calls++
	s.gotPages = pages
	s.gotMax = maxPages
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubPDFium) Path() string { return "stub-pdfium" }

func pdfiumResult() *pdfium.Result {
	return &pdfium.Result{
		Title:      "pdfium paper",
		PageCount:  3,
		Pages:      []int{1, 2},
		Markdown:   "pdfium text",
		TotalLines: 1,
		TotalChars: len("pdfium text"),
	}
}

func TestParseLocalPDF_PDFiumFallback(t *testing.T) {
	engine := &stubEngine{pdfErr: errors.New("no text extracted")}
	backend := &stubPDFium{result: pdfiumResult()}
	fetcher := &Fetcher{engine: engine, pdfiumBackend: backend, pdfiumMode: pdfium.ModeFallback}

	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "pdfium" || result.Title != "pdfium paper" || result.ParsedPages != 2 {
		t.Fatalf("result = %+v", result)
	}
	if backend.calls != 1 || engine.pdfCalls != 1 {
		t.Fatalf("pdfium calls=%d pdfCalls=%d", backend.calls, engine.pdfCalls)
	}
}

func TestParseLocalPDF_PDFiumPrimarySkipsLibrary(t *testing.T) {
	engine := &stubEngine{}
	backend := &stubPDFium{result: pdfiumResult()}
	fetcher := &Fetcher{engine: engine, pdfiumBackend: backend, pdfiumMode: pdfium.ModePrimary}

	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "pdfium" {
		t.Fatalf("result = %+v", result)
	}
	if engine.pdfCalls != 0 {
		t.Fatalf("primary mode must not call the Go library, pdfCalls=%d", engine.pdfCalls)
	}
}

func TestParseLocalPDF_PDFiumPrimaryFallsBack(t *testing.T) {
	engine := &stubEngine{}
	backend := &stubPDFium{err: errors.New("pdfium failed")}
	fetcher := &Fetcher{engine: engine, pdfiumBackend: backend, pdfiumMode: pdfium.ModePrimary}

	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "pdf-lib" || engine.pdfCalls != 1 || backend.calls != 1 {
		t.Fatalf("result=%+v pdfCalls=%d pdfiumCalls=%d", result, engine.pdfCalls, backend.calls)
	}
}

func TestParseLocalPDF_PDFiumFallbackUnavailableKeepsError(t *testing.T) {
	engine := &stubEngine{pdfErr: errors.New("broken pdf")}
	backend := &stubPDFium{err: pdfium.ErrUnavailable}
	fetcher := &Fetcher{engine: engine, pdfiumBackend: backend, pdfiumMode: pdfium.ModeFallback}

	_, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err == nil || !strings.Contains(err.Error(), "broken pdf") {
		t.Fatalf("original error must be preserved, got %v", err)
	}
	if backend.calls != 1 {
		t.Fatalf("pdfium calls=%d", backend.calls)
	}
}

func TestParseLocalPDF_PDFiumFallbackKeepsGuardEscalation(t *testing.T) {
	dir := t.TempDir()
	engine := &stubEngine{pdfPanic: true}
	spy := &spyMineru{
		hasToken:    true,
		localResult: mineruLocalResult(t, dir),
	}
	backend := &stubPDFium{err: errors.New("pdfium failed")}
	fetcher := &Fetcher{engine: engine, mineru: spy, pdfiumBackend: backend, pdfiumMode: pdfium.ModeFallback}

	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "mineru-local" || spy.localCalls != 1 || backend.calls != 1 {
		t.Fatalf("result=%+v localCalls=%d pdfiumCalls=%d", result, spy.localCalls, backend.calls)
	}
}

func TestParseLocalPDF_PDFiumFallbackNoTextStillOCR(t *testing.T) {
	engine := &stubEngine{pdfErr: errors.New("no text extracted")}
	spy := &spyMineru{hasToken: true}
	backend := &stubPDFium{err: pdfium.ErrNoText}
	fetcher := &Fetcher{engine: engine, mineru: spy, mineruOCR: true, pdfiumBackend: backend, pdfiumMode: pdfium.ModeFallback}

	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "mineru-ocr" || spy.parseFileCalls != 1 || backend.calls != 1 {
		t.Fatalf("result=%+v parseFileCalls=%d pdfiumCalls=%d", result, spy.parseFileCalls, backend.calls)
	}
}

func TestNewFromConfig_PDFiumDisabledExplicitly(t *testing.T) {
	fetcher, err := NewFromConfig(config.CleanFetchConfig{FileOutputDir: t.TempDir()}, config.PDFParserConfig{
		PDFium: config.PDFiumConfig{Enabled: pdfiumBoolPtr(false)},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()
	if fetcher.pdfiumBackend != nil {
		t.Fatal("explicit enabled=false must disable the backend")
	}
}

func TestNewFromConfig_PDFiumAutoDetect(t *testing.T) {
	fetcher, err := NewFromConfig(config.CleanFetchConfig{FileOutputDir: t.TempDir()}, config.PDFParserConfig{}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()
	if fetcher.pdfiumBackend == nil || fetcher.pdfiumMode != pdfium.ModeFallback {
		t.Fatalf("auto-detect backend=%v mode=%q", fetcher.pdfiumBackend, fetcher.pdfiumMode)
	}
}

func TestNewFromConfig_PDFiumEnabledLazy(t *testing.T) {
	fetcher, err := NewFromConfig(config.CleanFetchConfig{FileOutputDir: t.TempDir()}, config.PDFParserConfig{
		PDFium: config.PDFiumConfig{Enabled: pdfiumBoolPtr(true), Path: "missing-pdfium.dll"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()
	if fetcher.pdfiumBackend == nil || fetcher.pdfiumMode != pdfium.ModeFallback {
		t.Fatalf("backend=%v mode=%q", fetcher.pdfiumBackend, fetcher.pdfiumMode)
	}
	if !strings.Contains(fetcher.pdfiumBackend.Path(), "missing-pdfium.dll") {
		t.Fatalf("path = %q", fetcher.pdfiumBackend.Path())
	}
}

func pdfiumBoolPtr(b bool) *bool { return &b }

func mineruLocalResult(t *testing.T, dir string) *mineru.LocalResult {
	t.Helper()
	md := filepath.Join(dir, "full.md")
	if err := os.WriteFile(md, []byte("# mineru archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return &mineru.LocalResult{
		Directory: dir, Markdown: md, Source: "x.pdf",
		Pages: []int{1}, TotalPages: 1, Complete: true,
	}
}
