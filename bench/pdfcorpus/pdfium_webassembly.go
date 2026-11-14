package main

import (
	"fmt"
	"io"
	"time"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// pdfiumWASM runs the same PDFium build compiled to WebAssembly on wazero.
// The pool is compiled once per worker process and the single instance is
// reused across documents, matching a long-lived sidecar.
type pdfiumWASM struct {
	pool pdfium.Pool
	inst pdfium.Pdfium
}

func newPDFiumWASM() (*pdfiumWASM, error) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:      1,
		MaxIdle:      1,
		MaxTotal:     1,
		Stderr:       io.Discard,
		Stdout:       io.Discard,
		FSConfig:     nil,
		RandomSource: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("init wasm pdfium: %w", err)
	}
	inst, err := pool.GetInstance(120 * time.Second)
	if err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("get wasm instance: %w", err)
	}
	return &pdfiumWASM{pool: pool, inst: inst}, nil
}

func (w *pdfiumWASM) Extract(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty file")
	}
	doc, err := w.inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = w.inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	count, err := w.inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return "", err
	}
	pages := make([]string, 0, count.PageCount)
	for i := 0; i < count.PageCount; i++ {
		text, err := w.inst.GetPageText(&requests.GetPageText{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}},
		})
		if err != nil {
			return "", err
		}
		pages = append(pages, text.Text)
	}
	return joinPageText(pages)
}

func (w *pdfiumWASM) Close() {
	if w.inst != nil {
		_ = w.inst.Close()
	}
	if w.pool != nil {
		_ = w.pool.Close()
	}
}
