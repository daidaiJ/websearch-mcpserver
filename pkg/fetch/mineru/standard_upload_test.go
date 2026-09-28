package mineru

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStandardFileUploadsCroppedPDF(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	md, _ := zw.Create("full.md")
	_, _ = md.Write([]byte("# Parsed\n![figure](images/figure.png)"))
	image, _ := zw.Create("images/figure.png")
	_, _ = image.Write([]byte("image"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	uploaded := false
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/file-urls/batch":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing MinerU token")
			}
			var body struct {
				Files []struct {
					Name string `json:"name"`
				} `json:"files"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Files) != 1 || body.Files[0].Name != "selected.pdf" {
				t.Errorf("upload request files: %+v", body.Files)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"batch_id": "batch-1", "file_urls": []string{server.URL + "/upload"},
			}})
		case "/upload":
			data, _ := io.ReadAll(r.Body)
			uploaded = r.Method == http.MethodPut && bytes.HasPrefix(data, []byte("%PDF-"))
		case "/api/v4/extract-results/batch/batch-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"extract_result": []map[string]any{{"state": "done", "full_zip_url": server.URL + "/result.zip"}},
			}})
		case "/result.zip":
			_, _ = w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "selected.pdf")
	if err := os.WriteFile(file, testOnePagePDF(), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewFromConfig("test-token", "pipeline", "ch", false, true, true, "")
	client.endpoint = server.URL
	result, err := client.ParseStandardFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if !uploaded || !strings.Contains(result, server.URL+"/result.zip") || !strings.Contains(result, "images/figure.png") {
		t.Fatalf("upload=%v result=%q", uploaded, result)
	}
}
