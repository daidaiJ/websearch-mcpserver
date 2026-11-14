package webfetch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
)

// TestParseLocalPDF_RealPDFiumPrimary exercises the production wiring with the
// actual shared library. It is skipped when the library is not installed.
func TestParseLocalPDF_RealPDFiumPrimary(t *testing.T) {
	dll := realPDFiumPath()
	if dll == "" {
		t.Skip("PDFium dynamic library not available; set WEBSEARCH_PDFIUM_DLL to run")
	}
	path := filepath.Join(t.TempDir(), "real.pdf")
	text := "retained text"
	if err := os.WriteFile(path, syntheticTextPDF(text), 0600); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		FileOutputDir: t.TempDir(), MaxInlineLines: 100,
	}, config.PDFParserConfig{
		PDFium: config.PDFiumConfig{Enabled: pdfiumBoolPtr(true), Path: dll, Mode: "primary"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()

	result, err := fetcher.parseLocalPDF(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "pdfium" {
		t.Fatalf("ParseEngine = %q", result.ParseEngine)
	}
	if !strings.Contains(result.Markdown, text) {
		t.Fatalf("markdown = %q", result.Markdown)
	}
	if result.PageCount != 1 || result.ParsedPages != 1 {
		t.Fatalf("pages = %+v", result)
	}
}

// TestParseLocalPDF_RealPDFiumFallback exercises the user-facing fallback:
// the built-in parser panics on trailing newlines, so PDFium gets one chance
// before the error/guard/MinerU chain.
func TestParseLocalPDF_RealPDFiumFallback(t *testing.T) {
	dll := realPDFiumPath()
	if dll == "" {
		t.Skip("PDFium dynamic library not available; set WEBSEARCH_PDFIUM_DLL to run")
	}
	path := filepath.Join(t.TempDir(), "trailing-newlines.pdf")
	data := append(syntheticTextPDF("retained text"), bytes.Repeat([]byte("\n"), 120)...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		FileOutputDir: t.TempDir(), MaxInlineLines: 100,
	}, config.PDFParserConfig{
		PDFium: config.PDFiumConfig{Enabled: pdfiumBoolPtr(true), Path: dll, Mode: "fallback"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()

	result, err := fetcher.parseLocalPDF(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ParseEngine != "pdfium" {
		t.Fatalf("ParseEngine = %q", result.ParseEngine)
	}
	if !strings.Contains(result.Markdown, "retained text") {
		t.Fatalf("markdown = %q", result.Markdown)
	}
}

func realPDFiumPath() string {
	if p := os.Getenv("WEBSEARCH_PDFIUM_DLL"); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	for _, p := range []string{"pdfium.dll", "libpdfium.so", "libpdfium.dylib",
		`D:/tmp/pdfcorpus-tools/pdfium/bin/pdfium.dll`} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func syntheticTextPDF(text string) []byte {
	content := "BT /F1 12 Tf (" + text + ") Tj ET"
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}
