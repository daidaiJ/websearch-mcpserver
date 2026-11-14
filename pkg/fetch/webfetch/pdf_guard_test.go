package webfetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/errcode"
	"websearch/pkg/fetch/mineru"
)

func TestParseLocalPDF_GuardRecoversPanic(t *testing.T) {
	engine := &stubEngine{pdfPanic: true}
	fetcher := &Fetcher{engine: engine}
	_, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if !isPDFGuardFailure(err) {
		t.Fatalf("panic must become a guard failure: %v", err)
	}
	if errcode.CodeOf(err) != errcode.Parse {
		t.Fatalf("code=%q", errcode.CodeOf(err))
	}
	if engine.pdfCalls != 1 {
		t.Fatalf("pdfCalls=%d", engine.pdfCalls)
	}
}

func TestParseLocalPDF_GuardPanicEscalatesToMinerU(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "full.md")
	if err := os.WriteFile(md, []byte("# mineru archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &stubEngine{pdfPanic: true}
	spy := &spyMineru{
		hasToken: true,
		localResult: &mineru.LocalResult{
			Directory: dir, Markdown: md, Source: "x.pdf",
			Pages: []int{1}, TotalPages: 1, Complete: true,
		},
	}
	fetcher := &Fetcher{engine: engine, mineru: spy}
	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatalf("escalation failed: %v", err)
	}
	if result.ParseEngine != "mineru-local" || spy.localCalls != 1 || engine.pdfCalls != 1 {
		t.Fatalf("result=%+v spy.localCalls=%d pdfCalls=%d", result, spy.localCalls, engine.pdfCalls)
	}
	if result.Preamble == "" {
		t.Fatal("escalation must explain why MinerU ran")
	}
}

func TestParseLocalPDF_GuardTimeoutNoToken(t *testing.T) {
	engine := &stubEngine{pdfHang: true}
	fetcher := &Fetcher{engine: engine}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := fetcher.parseLocalPDF(ctx, "x.pdf")
	if !isPDFGuardFailure(err) {
		t.Fatalf("hang must become a guard failure: %v", err)
	}
}

func TestParseLocalPDF_GuardTimeoutEscalatesToMinerU(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "full.md")
	if err := os.WriteFile(md, []byte("# mineru archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &stubEngine{pdfHang: true}
	spy := &spyMineru{
		hasToken: true,
		localResult: &mineru.LocalResult{
			Directory: dir, Markdown: md, Source: "x.pdf",
			Pages: []int{1}, TotalPages: 1, Complete: true,
		},
	}
	fetcher := &Fetcher{engine: engine, mineru: spy}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	result, err := fetcher.parseLocalPDF(ctx, "x.pdf")
	if err != nil {
		t.Fatalf("timeout escalation failed: %v", err)
	}
	if result.ParseEngine != "mineru-local" || spy.localCalls != 1 {
		t.Fatalf("result=%+v spy=%+v", result, spy)
	}
}

func TestParseLocalPDF_SuccessUnchanged(t *testing.T) {
	engine := &stubEngine{}
	fetcher := &Fetcher{engine: engine}
	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil || result.ParseEngine != "pdf-lib" || result.Markdown != "stub" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestParseLocalPDF_OCRStillUsedForNoText(t *testing.T) {
	engine := &stubEngine{pdfErr: errors.New("no text extracted")}
	spy := &spyMineru{hasToken: true}
	fetcher := &Fetcher{engine: engine, mineru: spy, mineruOCR: true}
	result, err := fetcher.parseLocalPDF(context.Background(), "x.pdf")
	if err != nil {
		t.Fatalf("OCR fallback failed: %v", err)
	}
	if result.ParseEngine != "mineru-ocr" || spy.parseFileCalls != 1 || spy.localCalls != 0 {
		t.Fatalf("result=%+v spy.parseFileCalls=%d localCalls=%d", result, spy.parseFileCalls, spy.localCalls)
	}
}
