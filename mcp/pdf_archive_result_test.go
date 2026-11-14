package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"websearch/pkg/fetch/webfetch"
)

func TestPersistentPDFPackageGetsFreshReferenceForOldMarkdown(t *testing.T) {
	initTestLogger()
	path := filepath.Join(t.TempDir(), "full.md")
	const body = "# 长期保存的解析包\n公式与图片链接\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	notice := savedResultReadNotice(&webfetch.Result{FilePath: path, Persistent: true})
	ref := regexp.MustCompile(`result:[0-9a-f]{32}`).FindString(notice)
	if ref == "" {
		t.Fatalf("old persistent file could not be registered: %s", notice)
	}
	_, output, err := readSavedResult(context.Background(), nil, &ReadResultParams{Ref: ref})
	if err != nil || output.Content != body || !output.EOF {
		t.Fatalf("read=%+v err=%v", output, err)
	}
}
