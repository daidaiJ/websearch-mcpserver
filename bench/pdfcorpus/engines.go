package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	fastpdf "github.com/AOShei/go-fast-pdf/pkg/loader"
	aspose "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"
	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	razvan "github.com/razvandimescu/gopdf/pdf"
)

func extractGo(engine, path string) (string, error) {
	switch engine {
	case "ledongthuc":
		return extractLedong(path)
	case "pdfcpu":
		return extractPDFCPU(path)
	case "go-fitz":
		return extractFitz(path)
	case "aspose":
		return extractAspose(path)
	case "razvan":
		return extractRazvan(path)
	case "gofastpdf":
		return extractFastPDF(path)
	case "pdfium-dll":
		return extractPDFiumDLL(path)
	case "pdfium-wasm":
		return extractPDFiumWASM(path)
	default:
		return "", fmt.Errorf("unknown go engine %s", engine)
	}
}

func extractPDFiumDLL(path string) (string, error) {
	ext, err := openPDFiumDLL()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ext.Extract(data)
}

func extractPDFiumWASM(path string) (string, error) {
	ext, err := newPDFiumWASM()
	if err != nil {
		return "", err
	}
	defer ext.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ext.Extract(data)
}

func extractLedong(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return ledongText(r)
}

func extractLedongBytes(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	return ledongText(r)
}

func ledongText(r *pdf.Reader) (string, error) {
	n := r.NumPage()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(text)
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("no text")
	}
	return b.String(), nil
}

func extractPDFCPU(path string) (string, error) {
	ctx, err := api.ReadContextFile(context.Background(), path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 1; i <= ctx.PageCount; i++ {
		rd, err := pdfcpu.ExtractPageContent(context.Background(), ctx, i)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rd)
		if err != nil {
			continue
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("no content stream")
	}
	return b.String(), nil
}

func extractFitz(path string) (string, error) {
	return extractMuPDF(path)
}

func extractAspose(path string) (string, error) {
	doc, err := aspose.Open(path)
	if err != nil {
		return "", err
	}
	texts, err := doc.ExtractText()
	if err != nil {
		return "", err
	}
	text := strings.Join(texts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no text")
	}
	return text, nil
}

func extractRazvan(path string) (string, error) {
	doc, err := razvan.OpenFile(path)
	if err != nil {
		return "", err
	}
	text, err := doc.Text()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no text")
	}
	return text, nil
}

func extractFastPDF(path string) (string, error) {
	doc, err := fastpdf.LoadPDF(path, false)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, page := range doc.Pages {
		b.WriteString(page.Content)
		b.WriteByte('\n')
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", fmt.Errorf("no text")
	}
	return b.String(), nil
}
