package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
)

type fixture struct {
	Name     string
	Source   string
	Want     string // substring expected if the extractor survives
	WantForm string // extra: form XObject text
	Data     []byte
}

func fixtures() []fixture {
	retained := "BT /F1 12 Tf (retained text) Tj ET"
	return []fixture{
		{Name: "healthy", Source: "control", Want: "retained text", Data: contentPDF(retained)},
		{Name: "inline_image", Source: "ledongthuc#57 #89 BI/EI", Want: "retained text",
			Data: contentPDF(retained + "\nBI /W 1 /H 1 /BPC 1 /CS /G ID\n\x00\x00EI")},
		{Name: "bad_tf", Source: "ledongthuc#92 Tf operands", Want: "retained text",
			Data: contentPDF("BT /F1 Tf (retained text) Tj ET")},
		{Name: "empty_tj", Source: "ledongthuc#92 empty TJ",
			Data: contentPDF("BT /F1 12 Tf TJ ET")},
		{Name: "quote_operator", Source: "ledongthuc#92 quote op", Want: "retained text",
			Data: contentPDF("BT /F1 12 Tf 1 2 (retained text) \" ET")},
		{Name: "extra_q", Source: "ledongthuc#78 empty Q stack", Want: "retained text",
			Data: contentPDF("Q Q " + retained)},
		{Name: "unterminated_array", Source: "ledongthuc#80 readArray EOF", Want: "retained text",
			Data: contentPDF(retained + " [ (truncated) 1 2")},
		{Name: "unterminated_hex", Source: "ledongthuc#78 hex loop",
			Data: contentPDF("BT /F1 12 Tf <4142")},
		{Name: "split_stream", Source: "ledongthuc#76 cross-stream array", Want: "retained text",
			Data: contentPDF("BT /F1 12 Tf [(retained)", "( text)] TJ ET")},
		{Name: "trailing_newlines", Source: "ledongthuc#78 CR/LF strip", Want: "retained text",
			Data: append(contentPDF(retained), bytes.Repeat([]byte("\n"), 120)...)},
		{Name: "missing_eof", Source: "ledongthuc#20 missing %%EOF", Want: "retained text",
			Data: bytes.TrimSuffix(contentPDF(retained), []byte("%%EOF\n"))},
		{Name: "negative_startxref", Source: "ledongthuc#92 negative startxref",
			Data: withStartxref(contentPDF(retained), -1)},
		{Name: "huge_size", Source: "ledongthuc#78 /Size 2^62",
			Data: withTrailerSize(contentPDF(retained), "4611686018427387904")},
		{Name: "tj_kerning", Source: "ledongthuc#103 pdfTeX TJ gaps", Want: "retained",
			Data: contentPDF("BT /F1 12 Tf [(retained) -250 (text)] TJ ET")},
		{Name: "form_xobject", Source: "ledongthuc#67 #97 Do form", Want: "retained text", WantForm: "form text",
			Data: formXObjectPDF()},
		{Name: "cyclic_pages", Source: "ledongthuc#92 cyclic /Kids",
			Data: cyclicPagesPDF()},
		{Name: "empty_page", Source: "empty contents",
			Data: contentPDF("")},
		{Name: "indirect_kids", Source: "aspose#2 /Kids as ref", Want: "retained text",
			Data: indirectKidsPDF(retained)},
		{Name: "stray_brace", Source: "razvan#42 stray }", Want: "retained text",
			Data: contentPDF("BT /F1 12 Tf 10 100 Td [(retained text) } ] TJ ET")},
		{Name: "deep_dict", Source: "razvan#42 nested dict", Want: "retained text",
			Data: deepDictPDF(retained)},
		{Name: "circular_prev", Source: "pypdf#3654 /Prev cycle", Want: "retained text",
			Data: circularPrevPDF(retained)},
		{Name: "parent_number", Source: "pypdf#4202 /Parent number", Want: "retained text",
			Data: parentNumberPDF(retained)},
		{Name: "comment_storm", Source: "pypdf#4145 % comments", Want: "retained text",
			Data: contentPDF(commentStorm(retained))},
		{Name: "self_length", Source: "pypdf#3112 /Length self-ref", Want: "retained text",
			Data: selfLengthPDF(retained)},
		{Name: "empty_kids", Source: "pdfcpu#1494 empty /Kids",
			Data: emptyKidsPDF(retained)},
		{Name: "huge_length", Source: "razvan#44 claimed /Length", Want: "retained text",
			Data: hugeLengthPDF(retained, 32*1024*1024)},
		{Name: "cyclic_form", Source: "pdfminer#1113 cyclic Form", Want: "retained text",
			Data: cyclicFormPDF(retained)},
		{Name: "count_mismatch", Source: "pages /Count vs /Kids", Want: "retained text",
			Data: countMismatchPDF(retained, 999999)},
		{Name: "nested_q", Source: "ledongthuc#92 q stack", Want: "retained text",
			Data: contentPDF(strings.Repeat("q ", 400) + retained)},
		{Name: "flate_ok", Source: "normal FlateDecode", Want: "retained text",
			Data: multiPagePDF(1, retained, true)},
		{Name: "flate_garbage", Source: "bad FlateDecode stream",
			Data: flateGarbagePDF()},
		{Name: "null_kid", Source: "Kids [null page]", Want: "retained text",
			Data: nullKidPDF(retained)},
		{Name: "missing_endstream", Source: "stream without endstream", Want: "retained text",
			Data: missingEndstreamPDF(retained)},
		{Name: "truncated_xref", Source: "cut xref table",
			Data: truncatedXrefPDF(retained)},
	}
}

func benchFixtures() []fixture {
	line := "BT /F1 12 Tf (" + strings.Repeat("retained text ", 40) + ") Tj ET"
	return []fixture{
		{Name: "bench_1p", Source: "perf 1 page uncompressed", Want: "retained text", Data: multiPagePDF(1, line, false)},
		{Name: "bench_20p", Source: "perf 20 page uncompressed", Want: "retained text", Data: multiPagePDF(20, line, false)},
		{Name: "bench_20p_flate", Source: "perf 20 page FlateDecode", Want: "retained text", Data: multiPagePDF(20, line, true)},
	}
}

func fontRes() string {
	return "/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>"
}

func contentPDF(contents ...string) []byte {
	var refs []string
	for i := range contents {
		refs = append(refs, fmt.Sprintf("%d 0 R", i+4))
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents [%s] >>", fontRes(), strings.Join(refs, " ")),
	}
	for _, content := range contents {
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	return objectsPDF(objects)
}

func multiPagePDF(n int, content string, flate bool) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"", // pages, filled below
	}
	kids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		pageObj := 3 + 2*i
		contentObj := pageObj + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObj))
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents %d 0 R >>", fontRes(), contentObj))
		if flate {
			objects = append(objects, flateContentObject(content))
		} else {
			objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		}
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	return objectsPDF(objects)
}

func cyclicPagesPDF() []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [2 0 R] /Count 1 >>",
	})
}

func formXObjectPDF() []byte {
	form := "BT /F1 12 Tf (form text) Tj ET"
	page := "BT /F1 12 Tf (retained text) Tj ET /Fm1 Do"
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> /XObject << /Fm1 5 0 R >> >> /Contents 4 0 R >>"),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] %s /Length %d >>\nstream\n%s\nendstream", fontRes(), len(form), form),
	})
}

func indirectKidsPDF(content string) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids 5 0 R /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[3 0 R]",
	})
}

func deepDictPDF(content string) []byte {
	nest := strings.Repeat("<< /A ", 120) + "1" + strings.Repeat(" >>", 120)
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		nest,
	})
}

func circularPrevPDF(content string) []byte {
	data := contentPDF(content)
	xrefOff := startxrefValue(data)
	if xrefOff == "" {
		return data
	}
	return bytes.Replace(data, []byte("trailer\n<< "), []byte("trailer\n<< /Prev "+xrefOff+" "), 1)
}

func parentNumberPDF(content string) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 0 /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

func commentStorm(content string) string {
	var b strings.Builder
	b.WriteString("BT /F1 12 Tf\n")
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&b, "%% comment %d should be ignored\n", i)
	}
	b.WriteString("(retained text) Tj ET")
	_ = content
	return b.String()
}

func selfLengthPDF(content string) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length 4 0 R >>\nstream\n%s\nendstream", content),
	})
}

func emptyKidsPDF(content string) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

func hugeLengthPDF(content string, claim int) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", claim, content),
	})
}

func cyclicFormPDF(content string) []byte {
	form := "/Fm1 Do"
	page := content + " /Fm1 Do"
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> /XObject << /Fm1 5 0 R >> >> /Contents 4 0 R >>"),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Fm1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
	})
}

func countMismatchPDF(content string, count int) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [3 0 R] /Count %d >>", count),
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

func flateGarbagePDF() []byte {
	garbage := "this is not zlib data!!!!"
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(garbage), garbage),
	})
}

func nullKidPDF(content string) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [null 3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

func missingEndstreamPDF(content string) []byte {
	return objectsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Contents 4 0 R >>", fontRes()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\n", len(content), content),
	})
}

func truncatedXrefPDF(content string) []byte {
	data := contentPDF(content)
	i := bytes.LastIndex(data, []byte("xref\n"))
	if i < 0 {
		return data
	}
	keep := i + 24
	if keep > len(data) {
		keep = len(data) / 2
	}
	return data[:keep]
}

func flateContentObject(content string) string {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write([]byte(content))
	_ = w.Close()
	data := buf.Bytes()
	return fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(data), data)
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

func startxrefValue(data []byte) string {
	const key = "startxref\n"
	i := bytes.LastIndex(data, []byte(key))
	if i < 0 {
		return ""
	}
	rest := data[i+len(key):]
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		return strings.TrimSpace(string(rest))
	}
	return strings.TrimSpace(string(rest[:nl]))
}

func withTrailerSize(data []byte, size string) []byte {
	return bytes.Replace(data, []byte("/Size 5"), []byte("/Size "+size), 1)
}
