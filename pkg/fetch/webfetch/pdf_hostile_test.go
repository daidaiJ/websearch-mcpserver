package webfetch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"websearch/pkg/config"
)

// Upstream ledongthuc/pdf issues/PRs whose inputs previously crashed, hung,
// or exhausted memory. These fixtures keep the local parser from taking the
// process down; a guard abort may escalate to MinerU when a token is present.
func TestLocalPDFUpstreamHostileInputs(t *testing.T) {
	const childEnv = "WEBSEARCH_PDF_HOSTILE_CHILD"
	if childCase := os.Getenv(childEnv); childCase != "" {
		runHostileChild(t, childCase)
		return
	}

	for _, tc := range hostilePDFCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.isolate {
				runHostileIsolated(t, childEnv, tc.name)
				return
			}
			runHostileInProcess(t, tc)
		})
	}
}

type hostilePDFCase struct {
	name    string
	source  string // ledongthuc/pdf issue or PR
	data    []byte
	isolate bool // hang / fatal OOM: run in a child process
}

func hostilePDFCases() []hostilePDFCase {
	retained := "BT /F1 12 Tf (retained text) Tj ET"
	return []hostilePDFCase{
		{
			name:   "inline_image",
			source: "ledongthuc/pdf#57 #89 (BI/EI lexed as tokens)",
			data:   syntheticContentPDF(retained + "\nBI /W 1 /H 1 /BPC 1 /CS /G ID\n\x00\x00EI"),
		},
		{
			name:   "bad_tf",
			source: "ledongthuc/pdf#92 (Tf with missing operands panics)",
			data:   syntheticContentPDF("BT /F1 Tf (retained text) Tj ET"),
		},
		{
			name:   "empty_tj",
			source: "ledongthuc/pdf#92 (TJ with missing operands panics)",
			data:   syntheticContentPDF("BT /F1 12 Tf TJ ET"),
		},
		{
			name:   "quote_operator",
			source: "ledongthuc/pdf#92 (GetPlainText panics on \")",
			data:   syntheticContentPDF("BT /F1 12 Tf 1 2 (retained text) \" ET"),
		},
		{
			name:   "extra_q",
			source: "ledongthuc/pdf#78 (Q with empty graphics stack)",
			data:   syntheticContentPDF("Q Q " + retained),
		},
		{
			name:   "unterminated_array",
			source: "ledongthuc/pdf#80 PR #79 (readArray EOF loop)",
			data:   syntheticContentPDF(retained + " [ (truncated) 1 2"),
		},
		{
			name:   "unterminated_hex",
			source: "ledongthuc/pdf#78 lex_test (readHexString whitespace loop)",
			data:   syntheticContentPDF("BT /F1 12 Tf <4142"),
		},
		{
			name:   "trailing_newlines",
			source: "ledongthuc/pdf#78 (empty-buffer \\r/\\n strip panic)",
			data:   append(syntheticContentPDF(retained), bytes.Repeat([]byte("\n"), 120)...),
		},
		{
			name:   "negative_startxref",
			source: "ledongthuc/pdf#92 (negative startxref panics past Page recover)",
			data:   withStartxref(syntheticContentPDF(retained), -1),
		},
		{
			name:   "huge_size",
			source: "ledongthuc/pdf#78 (/Size 2^62 makeslice panic)",
			data:   withTrailerSize(syntheticContentPDF(retained), "4611686018427387904"),
		},
		{
			name:   "form_xobject",
			source: "ledongthuc/pdf#67 #97 (Do form XObject text ignored)",
			data:   formXObjectPDF(),
		},
		{
			name:    "cyclic_pages",
			source:  "ledongthuc/pdf#92 (page tree listing a node twice hangs)",
			data:    cyclicPagesPDF(),
			isolate: true,
		},
	}
}

func runHostileInProcess(t *testing.T, tc hostilePDFCase) {
	t.Helper()
	path := filepath.Join(t.TempDir(), tc.name+".pdf")
	if err := os.WriteFile(path, tc.data, 0600); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		FileOutputDir: t.TempDir(), MaxInlineLines: 100,
	}, config.PDFParserConfig{}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := fetcher.parseLocalPDF(ctx, path)
	if err == nil {
		if result == nil || result.Markdown == "" {
			t.Fatalf("%s (%s): empty success", tc.name, tc.source)
		}
		return
	}
	if ctx.Err() != nil && !isPDFGuardFailure(err) {
		t.Fatalf("%s (%s): parser did not return within 2s: %v", tc.name, tc.source, err)
	}
	// Guard abort, library error, or "no text" are all process-safe outcomes.
	t.Logf("%s (%s): safe failure: %v", tc.name, tc.source, err)
}

func runHostileIsolated(t *testing.T, childEnv, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalPDFUpstreamHostileInputs$")
	cmd.Env = append(os.Environ(), childEnv+"="+name)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s: child did not terminate within 5s: %s", name, output)
	}
	if err != nil {
		t.Fatalf("%s: child failed: %v\n%s", name, err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func runHostileChild(t *testing.T, name string) {
	cases := hostilePDFCases()
	var tc *hostilePDFCase
	for i := range cases {
		if cases[i].name == name {
			tc = &cases[i]
			break
		}
	}
	if tc == nil {
		t.Fatalf("unknown hostile case %q", name)
	}

	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			var current runtime.MemStats
			runtime.ReadMemStats(&current)
			if current.HeapAlloc > initial.HeapAlloc+64<<20 {
				fmt.Fprintln(os.Stderr, "PDF parsing exceeded the 64 MiB heap growth budget")
				os.Exit(2)
			}
		}
	}()

	dir, err := os.MkdirTemp("", "pdf-hostile-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".pdf")
	if err := os.WriteFile(path, tc.data, 0600); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewFromConfig(config.CleanFetchConfig{
		FileOutputDir: dir, MaxInlineLines: 100,
	}, config.PDFParserConfig{}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = fetcher.parseLocalPDF(ctx, path)
	if ctx.Err() != nil && err == nil {
		t.Fatalf("%s (%s): timed out without an error", name, tc.source)
	}
	fmt.Printf("%s: returned err=%v\n", name, err)
}

func withStartxref(data []byte, value int) []byte {
	const key = "startxref\n"
	i := bytes.LastIndex(data, []byte(key))
	if i < 0 {
		return data
	}
	rest := data[i+len(key):]
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		return data
	}
	var out bytes.Buffer
	out.Write(data[:i+len(key)])
	fmt.Fprintf(&out, "%d", value)
	out.Write(rest[nl:])
	return out.Bytes()
}

func withTrailerSize(data []byte, size string) []byte {
	old := []byte("/Size 5")
	return bytes.Replace(data, old, []byte("/Size "+size), 1)
}

func cyclicPagesPDF() []byte {
	// Node 2 lists node 3; node 3 lists node 2. Page() walks /Kids forever.
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [2 0 R] /Count 1 >>",
	})
}

func formXObjectPDF() []byte {
	form := "BT /F1 12 Tf (form text) Tj ET"
	page := "BT /F1 12 Tf (retained text) Tj ET /Fm1 Do"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Fm1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Length %d >>\nstream\n%s\nendstream", len(form), form),
	}
	return objectsPDF(objects)
}

func objectsPDF(objects []string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
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
