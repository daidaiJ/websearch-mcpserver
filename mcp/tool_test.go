package mcpserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/jina"
	"websearch/pkg/webfetch"
)

// ── CleanFetch handler 回退逻辑测试 ──────────────────────────────────────────

func TestCleanFetch_EmptyURL(t *testing.T) {
	_, _, err := CleanFetch(context.Background(), nil, &CleanFetchParams{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestCleanFetch_BothNil(t *testing.T) {
	oldWF := webfetchInst
	oldJina := jinaInst
	webfetchInst = nil
	jinaInst = nil
	defer func() { webfetchInst = oldWF; jinaInst = oldJina }()

	_, _, err := CleanFetch(context.Background(), nil, &CleanFetchParams{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error when both nil")
	}
	t.Logf("error: %v", err)
}

func TestCleanFetch_WebFetchSuccess(t *testing.T) {
	fetcher, err := webfetch.NewFromConfig(config.CleanFetchConfig{
		Enabled:        true,
		FileTTL:        1,
		MaxInlineLines: 100,
	})
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	defer fetcher.Close()

	oldWF := webfetchInst
	oldJina := jinaInst
	webfetchInst = fetcher
	jinaInst = nil
	defer func() { webfetchInst = oldWF; jinaInst = oldJina }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, _, err := CleanFetch(ctx, nil, &CleanFetchParams{URL: "https://arthurchiao.art/blog/k8s-scheduling-plugins-zh/"})
	if err != nil {
		t.Fatalf("CleanFetch failed: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected non-empty result")
	}
	t.Logf("Result content length: %d", len(fmt.Sprintf("%v", result.Content)))
}

func TestCleanFetch_WebFetchFail_JinaNil(t *testing.T) {
	fetcher, err := webfetch.NewFromConfig(config.CleanFetchConfig{
		Enabled:        true,
		FileTTL:        1,
		MaxInlineLines: 100,
	})
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	defer fetcher.Close()

	oldWF := webfetchInst
	oldJina := jinaInst
	webfetchInst = fetcher
	jinaInst = nil
	defer func() { webfetchInst = oldWF; jinaInst = oldJina }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 不存在的 URL，webfetch 会失败，jina 为 nil
	_, _, err = CleanFetch(ctx, nil, &CleanFetchParams{URL: "https://this-domain-does-not-exist-12345.com"})
	if err == nil {
		t.Fatal("expected error when webfetch fails and jina is nil")
	}
	t.Logf("error: %v", err)
}

func TestCleanFetch_OnlyJina(t *testing.T) {
	// 测试 webfetchInst 为 nil，仅 jinaInst 的路径
	// 由于 jinaInst 需要真实 API key，这里只验证逻辑分支
	oldWF := webfetchInst
	oldJina := jinaInst
	webfetchInst = nil
	// jinaInst 仍为 nil（无真实 key），所以两者都 nil
	jinaInst = nil
	defer func() { webfetchInst = oldWF; jinaInst = oldJina }()

	_, _, err := CleanFetch(context.Background(), nil, &CleanFetchParams{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error when both are nil")
	}
}

// ── formatWebFetchResult 测试 ─────────────────────────────────────────────────

func TestFormatWebFetchResult_Inline(t *testing.T) {
	result := &webfetch.Result{
		Title:    "Test Title",
		Mode:     "inline",
		Markdown: "Hello **world**",
	}
	r := formatWebFetchResult(result)
	if r == nil || len(r.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

func TestFormatWebFetchResult_SavedToFile(t *testing.T) {
	result := &webfetch.Result{
		Title:      "Big Doc",
		Mode:       "saved_to_file",
		FilePath:   "/tmp/webfetch/test.md",
		TotalLines: 500,
		TotalChars: 50000,
		AgentHint:  "Use read_file to read",
	}
	r := formatWebFetchResult(result)
	if r == nil || len(r.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

// ── formatJinaResult 测试 ─────────────────────────────────────────────────────

func TestFormatJinaResult(t *testing.T) {
	result := &jina.FetchResult{
		Title:         "Jina Title",
		Description:   "A description",
		PublishedTime: "2026-01-01",
		Content:       "Content body",
	}
	r := formatJinaResult(result)
	if r == nil || len(r.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

// ── PDFParserHandler 测试 ─────────────────────────────────────────────────────

func TestPDFParserHandler_EmptyPath(t *testing.T) {
	_, _, err := PDFParserHandler(context.Background(), nil, &PDFParserParams{Path: ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestPDFParserHandler_NotInitialized(t *testing.T) {
	oldWF := webfetchInst
	webfetchInst = nil
	defer func() { webfetchInst = oldWF }()

	_, _, err := PDFParserHandler(context.Background(), nil, &PDFParserParams{Path: "/tmp/test.pdf"})
	if err == nil {
		t.Fatal("expected error when webfetch not initialized")
	}
}
