package mineru

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCropPDFBeyondOriginalPageLimit(t *testing.T) {
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("qpdf is not installed")
	}
	one := filepath.Join(t.TempDir(), "one.pdf")
	if err := os.WriteFile(one, testOnePagePDF(), 0600); err != nil {
		t.Fatal(err)
	}
	many := filepath.Join(t.TempDir(), "many.pdf")
	repeated := strings.TrimSuffix(strings.Repeat("1,", 201), ",")
	if output, err := exec.Command("qpdf", "--empty", "--pages", one, repeated, "--", many).CombinedOutput(); err != nil {
		t.Fatalf("create 201-page PDF: %v: %s", err, output)
	}
	cropped, total, selected, cleanup, err := CropPDF(context.Background(), many, []int{2, 201}, 20, 200)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if total != 201 || selected != 2 {
		t.Fatalf("crop count total=%d selected=%d", total, selected)
	}
	output, err := exec.Command("qpdf", "--show-npages", cropped).Output()
	if err != nil || strings.TrimSpace(string(output)) != "2" {
		t.Fatalf("cropped PDF pages=%q err=%v", output, err)
	}
	if _, _, _, _, err := CropPDF(context.Background(), many, []int{202}, 20, 200); err == nil {
		t.Fatal("out-of-range page must fail")
	}
}

func testOnePagePDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\nendstream",
	}
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}
