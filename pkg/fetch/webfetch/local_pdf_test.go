package webfetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"websearch/pkg/fetch/mineru"
)

func TestLocalPDFArchiveDoesNotFallBackToText(t *testing.T) {
	pending := &mineru.TaskPendingError{TaskRef: "batch:local-job", Status: "running"}
	engine := &stubEngine{}
	spy := &spyMineru{hasToken: true, localErr: pending}
	fetcher := &Fetcher{engine: engine, mineru: spy}
	_, err := fetcher.ParseLocalPDFArchive(context.Background(), "file:///tmp/source.pdf", "", nil)
	if !errors.Is(err, pending) || spy.localCalls != 1 || engine.fetchCalls != 0 || engine.fetchWithOptsCalls != 0 {
		t.Fatalf("pending archival silently fell back to text: %v", err)
	}
	dir := t.TempDir()
	md := filepath.Join(dir, "full.md")
	if err := os.WriteFile(md, []byte("# 已有正文\n$table$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	spy.localResult = &mineru.LocalResult{Directory: dir, Markdown: md, Source: "source.pdf", Pages: []int{1, 2}, TotalPages: 2, Complete: true, Reused: true}
	spy.localErr = nil
	result, err := fetcher.ParseLocalPDFArchive(context.Background(), "file:///tmp/source.pdf", "", nil)
	if err != nil || !result.Persistent || result.FilePath != md || result.ParseEngine != "mineru-local" || result.ParsedPages != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
