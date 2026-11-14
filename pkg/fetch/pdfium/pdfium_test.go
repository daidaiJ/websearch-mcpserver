package pdfium

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractUnavailable(t *testing.T) {
	parser := New(Config{Path: filepath.Join(t.TempDir(), "missing-pdfium.dll")})
	path := filepath.Join(t.TempDir(), "valid.pdf")
	if err := os.WriteFile(path, testPDF("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := parser.Extract(context.Background(), path, nil, 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing library must return ErrUnavailable, got %v", err)
	}
}

func TestSelectPages(t *testing.T) {
	tests := []struct {
		name      string
		pageCount int
		requested []int
		maxPages  int
		want      []int
	}{
		{"all", 3, nil, 0, []int{1, 2, 3}},
		{"max pages", 5, nil, 2, []int{1, 2}},
		{"requested sorted deduped", 5, []int{3, 1, 3, 9, 0}, 0, []int{1, 3}},
		{"empty document", 0, nil, 0, nil},
		{"all requested out of range", 2, []int{3, 4}, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPages(tt.pageCount, tt.requested, tt.maxPages)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("selectPages = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	got := cleanText("a\r\n\r\n\r\nb \n")
	if got != "a\n\nb" {
		t.Fatalf("cleanText = %q", got)
	}
}

func TestBundledSubdir(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", filepath.Join("third_party", "pdfium", "win-x64")},
		{"windows", "arm64", filepath.Join("third_party", "pdfium", "win-arm64")},
		{"linux", "amd64", filepath.Join("third_party", "pdfium", "linux-x64")},
		{"linux", "arm64", filepath.Join("third_party", "pdfium", "linux-arm64")},
		{"darwin", "amd64", filepath.Join("third_party", "pdfium", "mac-x64")},
		{"darwin", "arm64", filepath.Join("third_party", "pdfium", "mac-arm64")},
		{"freebsd", "amd64", ""},
		{"linux", "386", ""},
	}
	for _, tt := range tests {
		if got := bundledSubdir(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("bundledSubdir(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestSearchDirs(t *testing.T) {
	dirs := SearchDirs("/exe", "/config")
	if len(dirs) < 2 || dirs[0] != "/exe" {
		t.Fatalf("SearchDirs = %v", dirs)
	}
	if sub := BundledSubdir(); sub != "" {
		want := filepath.Join("/exe", sub)
		if dirs[1] != want {
			t.Fatalf("bundled dir = %q, want %q (all: %v)", dirs[1], want, dirs)
		}
	}
	// 空目录与重复目录不得放大搜索列表。
	want := SearchDirs("/exe")
	if got := SearchDirs("", "/exe", "/exe"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("deduped SearchDirs = %v, want %v", got, want)
	}
}

func TestResolveBundledPath(t *testing.T) {
	sub := BundledSubdir()
	if sub == "" {
		t.Skip("platform has no bundled PDFium layout")
	}
	base := t.TempDir()
	bundled := filepath.Join(base, sub)
	if err := os.MkdirAll(bundled, 0755); err != nil {
		t.Fatal(err)
	}
	name := defaultLibraryName()
	if err := os.WriteFile(filepath.Join(bundled, name), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ResolvePath("", SearchDirs(base)); got != filepath.Join(bundled, name) {
		t.Fatalf("ResolvePath = %q", got)
	}
}

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	name := "pdfium-test-lib"
	if runtime.GOOS == "windows" {
		name += ".dll"
	} else {
		name += ".so"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	got := ResolvePath(name, []string{filepath.Join(dir, "missing"), dir})
	if got != filepath.Join(dir, name) {
		t.Fatalf("ResolvePath = %q", got)
	}
	abs := filepath.Join(dir, name)
	if got := ResolvePath(abs, nil); got != abs {
		t.Fatalf("absolute path = %q", got)
	}
	missing := ResolvePath("nope.dll", []string{dir})
	if missing != filepath.Join(dir, "nope.dll") {
		t.Fatalf("missing relative = %q", missing)
	}
}

func TestTitleFromText(t *testing.T) {
	if got := titleFromText("/tmp/paper.pdf", "# A Title\nbody"); got != "A Title" {
		t.Fatalf("title = %q", got)
	}
	if got := titleFromText("/tmp/paper.pdf", "\n\n"); got != "paper" {
		t.Fatalf("fallback title = %q", got)
	}
}

func TestExtractWithDLL(t *testing.T) {
	dll := testDLLPath()
	if dll == "" {
		t.Skip("PDFium dynamic library not available; set WEBSEARCH_PDFIUM_DLL to run")
	}
	parser := New(Config{Path: dll})
	path := filepath.Join(t.TempDir(), "two-pages.pdf")
	if err := os.WriteFile(path, testPDF("page one", "page two"), 0600); err != nil {
		t.Fatal(err)
	}

	all, err := parser.Extract(context.Background(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.PageCount != 2 || len(all.Pages) != 2 || all.Pages[0] != 1 || all.Pages[1] != 2 {
		t.Fatalf("page metadata = %+v", all)
	}
	if !strings.Contains(all.Markdown, "page one") || !strings.Contains(all.Markdown, "page two") {
		t.Fatalf("markdown missing page text: %q", all.Markdown)
	}
	if all.TotalChars == 0 || all.TotalLines == 0 {
		t.Fatalf("totals = %+v", all)
	}

	second, err := parser.Extract(context.Background(), path, []int{2}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Pages) != 1 || second.Pages[0] != 2 {
		t.Fatalf("selected pages = %v", second.Pages)
	}
	if strings.Contains(second.Markdown, "page one") || !strings.Contains(second.Markdown, "page two") {
		t.Fatalf("selected markdown = %q", second.Markdown)
	}
}

func testDLLPath() string {
	if p := os.Getenv("WEBSEARCH_PDFIUM_DLL"); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	candidates := []string{"pdfium.dll", "libpdfium.so", "libpdfium.dylib",
		`D:/tmp/pdfcorpus-tools/pdfium/bin/pdfium.dll`}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// testPDF builds a minimal valid multi-page PDF with Helvetica text.
func testPDF(pages ...string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"", // pages root, filled below
	}
	kids := make([]string, 0, len(pages))
	for i := range pages {
		pageObj := 3 + 2*i
		contentObj := pageObj + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObj))
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> /Contents %d 0 R >>", contentObj),
			fmt.Sprintf("<< /Length %d >>\nstream\nBT /F1 12 Tf (%s) Tj ET\nendstream", len("BT /F1 12 Tf ("+pages[i]+") Tj ET"), pages[i]))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))

	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}
