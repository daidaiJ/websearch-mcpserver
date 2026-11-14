package mineru

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type localMineruFixture struct {
	server  *httptest.Server
	created atomic.Int32
	queries atomic.Int32
	running atomic.Bool
}

func newLocalMineruFixture(t *testing.T) *localMineruFixture {
	t.Helper()
	f := &localMineruFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/file-urls/batch":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["enable_formula"] != true || body["enable_table"] != true || body["model_version"] != "pipeline" {
				t.Errorf("missing precise parsing options: %v", body)
			}
			id := f.created.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"batch_id": fmt.Sprintf("local-%d", id), "file_urls": []string{fmt.Sprintf("%s/upload/%d", f.server.URL, id)},
			}})
		case strings.HasPrefix(r.URL.Path, "/upload/"):
			data, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPut || !bytes.HasPrefix(data, []byte("%PDF-")) {
				t.Error("not a signed PDF upload")
			}
		case strings.HasPrefix(r.URL.Path, "/api/v4/extract-results/batch/"):
			f.queries.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/api/v4/extract-results/batch/local-")
			state := "done"
			if f.running.Load() {
				state = "running"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"extract_result": []map[string]any{{"state": state, "full_zip_url": f.server.URL + "/result/" + id}},
			}})
		case strings.HasPrefix(r.URL.Path, "/result/"):
			id := strings.TrimPrefix(r.URL.Path, "/result/")
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			md, _ := zw.Create("document/full.md")
			_, _ = fmt.Fprintf(md, "# Batch %s\n\n$E=mc^2$\n\n<table><tr><td>value</td></tr></table>\n\n![figure](images/figure.png)\n<img src=\"images/figure.png\">", id)
			image, _ := zw.Create("document/images/figure.png")
			_, _ = fmt.Fprint(image, "figure-"+id)
			_ = zw.Close()
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *localMineruFixture) client(dir string) *Client {
	c := NewFromConfig("test-token", "pipeline", "ch", false, true, true, "", 0)
	c.endpoint = f.server.URL
	c.SetLocalTaskDir(dir)
	return c
}

func localTestPDF(t *testing.T, pages int) string {
	t.Helper()
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("qpdf is not installed")
	}
	dir := filepath.Join(t.TempDir(), "中文 PDF 资料")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	one, source := filepath.Join(dir, "one.pdf"), filepath.Join(dir, "论文.pdf")
	if err := os.WriteFile(one, testOnePagePDF(), 0600); err != nil {
		t.Fatal(err)
	}
	repeat := strings.TrimSuffix(strings.Repeat("1,", pages), ",")
	if output, err := exec.Command("qpdf", "--empty", "--pages", one, repeat, "--", source).CombinedOutput(); err != nil {
		t.Fatalf("create PDF: %v: %s", err, output)
	}
	return source
}

func TestLocalArchiveWholeDocumentBudgetRestartAndAssets(t *testing.T) {
	f := newLocalMineruFixture(t)
	source := localTestPDF(t, 3)
	index := t.TempDir()
	ctx := WithTaskWait(context.Background(), 0)
	first, err := f.client(index).ParseLocalArchive(ctx, source, "", nil, 1, 2)
	if err != nil || first == nil || first.Complete || len(first.Pages) != 2 || len(first.Remaining) != 1 || f.created.Load() != 2 {
		t.Fatalf("first=%+v created=%d err=%v", first, f.created.Load(), err)
	}
	// A new client and a package path stand in for a service restart and later Agent.
	second, err := f.client(index).ParseLocalArchive(ctx, first.Directory, "", nil, 1, 2)
	if err != nil || !second.Complete || len(second.Pages) != 3 || f.created.Load() != 3 {
		t.Fatalf("second=%+v created=%d err=%v", second, f.created.Load(), err)
	}
	markdown, err := os.ReadFile(second.Markdown)
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`!\[figure\]\(([^)]+)\)`).FindAllStringSubmatch(string(markdown), -1) {
		if _, err := os.Stat(filepath.Join(second.Directory, filepath.FromSlash(match[1]))); err != nil {
			t.Fatalf("broken image link %q: %v", match[1], err)
		}
	}
	if strings.Count(string(markdown), "# Batch ") != 3 || !strings.Contains(string(markdown), "$E=mc^2$") || !strings.Contains(string(markdown), "<table>") {
		t.Fatalf("lost parsed content: %s", markdown)
	}
	queries := f.queries.Load()
	third, err := f.client(index).ParseLocalArchive(ctx, source, "", nil, 1, 2)
	if err != nil || !third.Reused || third.Markdown != second.Markdown || f.created.Load() != 3 || f.queries.Load() != queries {
		t.Fatalf("cache=%+v created=%d queries=%d err=%v", third, f.created.Load(), f.queries.Load(), err)
	}
	files, err := filepath.Glob(filepath.Join(second.Directory, "generated", "*", "result.zip"))
	if err != nil || len(files) != 3 {
		t.Fatalf("missing original ZIPs: %v %v", files, err)
	}
}

func TestLocalArchiveDefaultIncludesPagesBeyond20(t *testing.T) {
	f := newLocalMineruFixture(t)
	source := localTestPDF(t, 25)
	result, err := f.client(t.TempDir()).ParseLocalArchive(WithTaskWait(context.Background(), 0), source, "", nil, 0, 600)
	if err != nil || result == nil || !result.Complete || len(result.Pages) != 25 || result.Pages[24] != 25 || f.created.Load() != 1 {
		t.Fatalf("whole-document archival was capped at the text-extraction default: %+v err=%v", result, err)
	}
}

func TestLocalArchivePendingUpdateKeepsOldResultAndResumesTask(t *testing.T) {
	f := newLocalMineruFixture(t)
	source, index := localTestPDF(t, 1), t.TempDir()
	ctx := WithTaskWait(context.Background(), 0)
	first, err := f.client(index).ParseLocalArchive(ctx, source, "", nil, 0, 600)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(first.Markdown)
	note := filepath.Join(first.Directory, "my-notes.md")
	if err := os.WriteFile(note, []byte("keep my notes"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("\n% updated source\n")
	_ = file.Close()
	f.running.Store(true)
	_, err = f.client(index).ParseLocalArchive(ctx, source, "", nil, 0, 600)
	var pending *TaskPendingError
	if !errors.As(err, &pending) || f.created.Load() != 2 {
		t.Fatalf("pending=%v created=%d", err, f.created.Load())
	}
	stillOld, _ := os.ReadFile(first.Markdown)
	if !bytes.Equal(old, stillOld) {
		t.Fatal("replaced the previous result before new parsing completed")
	}
	f.running.Store(false)
	result, err := f.client(index).ResumeLocalArchive(ctx, pending.TaskRef)
	if err != nil || result == nil || !result.Complete || f.created.Load() != 2 {
		t.Fatalf("resume=%+v err=%v created=%d", result, err, f.created.Load())
	}
	newBody, _ := os.ReadFile(result.Markdown)
	if bytes.Equal(old, newBody) || !strings.Contains(string(newBody), "Batch 2") {
		t.Fatal("source update did not replace generated Markdown")
	}
	if data, err := os.ReadFile(note); err != nil || string(data) != "keep my notes" {
		t.Fatal("deleted user notes")
	}
	files, _ := filepath.Glob(filepath.Join(result.Directory, "generated", "*", "result.zip"))
	if len(files) != 1 {
		t.Fatalf("obsolete generated archives were retained: %v", files)
	}
}

func TestLocalArchiveOverlappingPagesDoNotResubmit(t *testing.T) {
	f := newLocalMineruFixture(t)
	source := localTestPDF(t, 4)
	c := f.client(t.TempDir())
	ctx := WithTaskWait(context.Background(), 0)
	if _, err := c.ParseLocalArchive(ctx, source, "", []int{1, 2}, 0, 600); err != nil {
		t.Fatal(err)
	}
	result, err := c.ParseLocalArchive(ctx, source, "", []int{2, 3}, 0, 600)
	if err != nil || len(result.Pages) != 3 || f.created.Load() != 2 {
		t.Fatalf("result=%+v created=%d err=%v", result, f.created.Load(), err)
	}
	record, err := readLocalPackage(filepath.Join(result.Directory, "manifest.json"))
	if err != nil || len(record.Batches[1].Pages) != 1 || record.Batches[1].Pages[0] != 3 {
		t.Fatalf("resubmitted covered pages: %+v %v", record, err)
	}
}

func TestLocalArchiveGapFillingKeepsOriginalPageOrder(t *testing.T) {
	f := newLocalMineruFixture(t)
	source := localTestPDF(t, 3)
	c := f.client(t.TempDir())
	ctx := WithTaskWait(context.Background(), 0)
	if _, err := c.ParseLocalArchive(ctx, source, "", []int{1, 3}, 0, 600); err != nil {
		t.Fatal(err)
	}
	result, err := c.ParseLocalArchive(ctx, source, "", nil, 0, 600)
	if err != nil || len(result.Pages) != 3 || f.created.Load() != 3 {
		t.Fatalf("result=%+v created=%d err=%v", result, f.created.Load(), err)
	}
	data, err := os.ReadFile(result.Markdown)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	first, middle, last := strings.Index(body, "# Batch 1"), strings.Index(body, "# Batch 3"), strings.Index(body, "# Batch 2")
	if first < 0 || middle <= first || last <= middle {
		t.Fatalf("gap-filled pages were merged out of order: %s", body)
	}
}

func TestLocalArchiveUnwritableOutputDoesNotUpload(t *testing.T) {
	f := newLocalMineruFixture(t)
	source := localTestPDF(t, 1)
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := f.client(t.TempDir()).ParseLocalArchive(context.Background(), source, blocked, nil, 0, 600)
	if err == nil || f.created.Load() != 0 || !strings.Contains(err.Error(), "output_dir") {
		t.Fatalf("err=%v created=%d", err, f.created.Load())
	}
}

func TestLocalArchiveZIPRejectsTraversalAndSymlinks(t *testing.T) {
	for i, name := range []string{"../escape.md", `..\escape.md`, "link"} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			dir := t.TempDir()
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			header := &zip.FileHeader{Name: name}
			if name == "link" {
				header.SetMode(os.ModeSymlink | 0777)
			}
			file, _ := writer.CreateHeader(header)
			_, _ = file.Write([]byte("outside"))
			_ = writer.Close()
			path := filepath.Join(dir, "result.zip")
			if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := extractLocalZIP(path, dir); err == nil {
				t.Fatal("unsafe ZIP was extracted")
			}
		})
	}
}
