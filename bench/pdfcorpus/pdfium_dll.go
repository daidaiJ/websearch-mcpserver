package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// PDFium is loaded at runtime through purego so the bench binary keeps
// CGO_ENABLED=0. Only the small FPDF/FPDFText surface needed for plain text is
// bound.
type pdfiumDLL struct {
	handle uintptr

	initLibrary    func()
	destroyLibrary func()
	getLastError   func() uint64

	loadMemDocument func(data unsafe.Pointer, size int, password uintptr) uintptr
	getPageCount    func(doc uintptr) int
	loadPage        func(doc uintptr, index int) uintptr
	closePage       func(page uintptr)
	closeDocument   func(doc uintptr)

	textLoadPage   func(page uintptr) uintptr
	textClosePage  func(textPage uintptr)
	textCountChars func(textPage uintptr) int
	textGetText    func(textPage uintptr, start, count int, result unsafe.Pointer) int
}

var (
	dllOnce    sync.Once
	dllShared  *pdfiumDLL
	dllInitErr error
)

func openPDFiumDLL() (*pdfiumDLL, error) {
	dllOnce.Do(func() {
		dllShared, dllInitErr = loadPDFiumDLL()
	})
	if dllInitErr != nil {
		return nil, dllInitErr
	}
	return dllShared, nil
}

func pdfiumLibraryPath() string {
	if p := os.Getenv("PDFCORPUS_PDFIUM_DLL"); p != "" {
		return p
	}
	name := "pdfium.dll"
	if runtime.GOOS != "windows" {
		name = "libpdfium.so"
	}
	if self, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(self), name)
	}
	return name
}

func loadPDFiumDLL() (*pdfiumDLL, error) {
	path := pdfiumLibraryPath()
	handle, err := loadLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	d := &pdfiumDLL{handle: handle}
	if err := d.bind(); err != nil {
		_ = unloadLibrary(handle)
		return nil, err
	}
	d.initLibrary()
	return d, nil
}

func (d *pdfiumDLL) bind() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("bind pdfium symbols: %v", r)
		}
	}()
	purego.RegisterLibFunc(&d.initLibrary, d.handle, "FPDF_InitLibrary")
	purego.RegisterLibFunc(&d.destroyLibrary, d.handle, "FPDF_DestroyLibrary")
	purego.RegisterLibFunc(&d.getLastError, d.handle, "FPDF_GetLastError")
	purego.RegisterLibFunc(&d.loadMemDocument, d.handle, "FPDF_LoadMemDocument")
	purego.RegisterLibFunc(&d.getPageCount, d.handle, "FPDF_GetPageCount")
	purego.RegisterLibFunc(&d.loadPage, d.handle, "FPDF_LoadPage")
	purego.RegisterLibFunc(&d.closePage, d.handle, "FPDF_ClosePage")
	purego.RegisterLibFunc(&d.closeDocument, d.handle, "FPDF_CloseDocument")
	purego.RegisterLibFunc(&d.textLoadPage, d.handle, "FPDFText_LoadPage")
	purego.RegisterLibFunc(&d.textClosePage, d.handle, "FPDFText_ClosePage")
	purego.RegisterLibFunc(&d.textCountChars, d.handle, "FPDFText_CountChars")
	purego.RegisterLibFunc(&d.textGetText, d.handle, "FPDFText_GetText")
	return nil
}

func pdfiumLoadError(code uint64) string {
	switch code {
	case 0:
		return "no error"
	case 1:
		return "unknown error"
	case 2:
		return "file not found or could not be opened"
	case 3:
		return "file not in PDF format or corrupted"
	case 4:
		return "password required or incorrect"
	case 5:
		return "unsupported security scheme"
	case 6:
		return "page not found or content error"
	default:
		return fmt.Sprintf("FPDF error %d", code)
	}
}

func (d *pdfiumDLL) Extract(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty file")
	}
	doc := d.loadMemDocument(unsafe.Pointer(&data[0]), len(data), 0)
	if doc == 0 {
		code := d.getLastError()
		if code == 0 {
			return "", fmt.Errorf("FPDF_LoadMemDocument failed (no PDFium error code)")
		}
		return "", fmt.Errorf("FPDF_LoadMemDocument: %s", pdfiumLoadError(code))
	}
	defer d.closeDocument(doc)

	count := d.getPageCount(doc)
	pages := make([]string, 0, count)
	for i := 0; i < count; i++ {
		page := d.loadPage(doc, i)
		if page == 0 {
			continue
		}
		text := d.pageText(page)
		d.closePage(page)
		pages = append(pages, text)
	}
	return joinPageText(pages)
}

func (d *pdfiumDLL) pageText(page uintptr) string {
	textPage := d.textLoadPage(page)
	if textPage == 0 {
		return ""
	}
	defer d.textClosePage(textPage)
	n := d.textCountChars(textPage)
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	written := d.textGetText(textPage, 0, n+1, unsafe.Pointer(&buf[0]))
	if written <= 1 {
		return ""
	}
	if written > len(buf) {
		written = len(buf)
	}
	return decodeUTF16(buf[:written-1])
}

func (d *pdfiumDLL) Close() {
	// The library is process-wide for the bench worker; FPDF_DestroyLibrary is
	// called by the OS at exit. Calling it here would race with a second
	// extractor in the same process.
}
