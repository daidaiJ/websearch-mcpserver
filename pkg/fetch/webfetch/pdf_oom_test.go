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

func TestLocalPDFUnterminatedArray(t *testing.T) {
	const childEnv = "WEBSEARCH_PDF_REGRESSION_CHILD"
	if childCase := os.Getenv(childEnv); childCase != "" {
		entrypoint, scenario, _ := strings.Cut(childCase, ":")
		// A parser regression must fail the child before its allocations exhaust the host.
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

		contents := []string{"BT /F1 12 Tf (retained text) Tj ET"}
		switch scenario {
		case "unterminated_array":
			contents[0] += " [ (truncated) 1 2"
		case "split_stream":
			contents = []string{"BT /F1 12 Tf [(retained)", "( text)] TJ ET"}
		}
		path := filepath.Join(t.TempDir(), "synthetic.pdf")
		if err := os.WriteFile(path, syntheticContentPDF(contents...), 0600); err != nil {
			t.Fatal(err)
		}
		fetcher, err := NewFromConfig(config.CleanFetchConfig{
			FileOutputDir: t.TempDir(), MaxInlineLines: 100,
		}, config.PDFParserConfig{}, "")
		if err != nil {
			t.Fatal(err)
		}
		defer fetcher.Close()
		fileURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(path), "/")
		var result *Result
		if entrypoint == "pdf_parser" {
			result, err = fetcher.FetchPDFWithPages(context.Background(), fileURL, []int{1}, 1)
		} else {
			result, err = fetcher.Fetch(context.Background(), fileURL)
		}
		if err != nil {
			t.Fatalf("local PDF parsing failed: %v", err)
		}
		if result.PageCount != 1 || !strings.Contains(result.Markdown, "retained text") {
			t.Fatalf("local PDF lost its normal text: %+v", result)
		}
		var final runtime.MemStats
		runtime.ReadMemStats(&final)
		fmt.Printf("%s: heap growth %.2f MiB\n", childCase, float64(int64(final.HeapAlloc)-int64(initial.HeapAlloc))/(1<<20))
		return
	}

	for _, entrypoint := range []string{"cleanfetch", "pdf_parser"} {
		for _, scenario := range []string{"normal", "unterminated_array", "split_stream"} {
			childCase := entrypoint + ":" + scenario
			t.Run(childCase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalPDFUnterminatedArray$")
				cmd.Env = append(os.Environ(), childEnv+"="+childCase)
				output, err := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("local PDF parser did not terminate within 5s: %s", output)
				}
				if err != nil {
					t.Fatalf("local PDF parser child failed: %v\n%s", err, output)
				}
				t.Log(strings.TrimSpace(string(output)))
			})
		}
	}
}

func syntheticContentPDF(contents ...string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	var refs []string
	for i := range contents {
		refs = append(refs, fmt.Sprintf("%d 0 R", i+4))
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents [%s] >>", strings.Join(refs, " ")),
	}
	for _, content := range contents {
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
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
