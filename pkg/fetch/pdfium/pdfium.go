// Package pdfium provides an optional, lazily loaded PDFium text extractor.
//
// The library is loaded at runtime through purego, so the MCP process stays
// CGO-free. PDFium is not fully thread-safe; all calls are serialized by a
// process-wide mutex. The dynamic library is an experimental backend: when it
// is missing or fails to load, callers fall back to the built-in Go parser.
package pdfium

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"websearch/pkg/i18n"
	"websearch/pkg/log"

	"github.com/ebitengine/purego"
)

// ErrUnavailable marks a missing dynamic library. Callers can fall back
// without surfacing an error to the user.
var ErrUnavailable = errors.New("pdfium library unavailable")

// ErrNoText marks a successfully opened document with no text layer.
var ErrNoText = errors.New("no text extracted")

const (
	// ModeFallback tries PDFium only after the built-in parser fails.
	ModeFallback = "fallback"
	// ModePrimary tries PDFium before the built-in parser.
	ModePrimary = "primary"
)

// Config controls the lazy loader and path resolution.
type Config struct {
	// Path is an absolute path, a relative path, or empty. Empty selects the
	// platform default (pdfium.dll / libpdfium.so / libpdfium.dylib).
	Path string
	// SearchDirs resolves a relative Path in order. Callers typically pass
	// the executable directory and the config directory.
	SearchDirs []string
	// WarnIfMissing logs when the library is absent. Auto-detection passes
	// false so deployments without the optional DLL stay quiet; an explicit
	// enable passes true so a misconfiguration is visible.
	WarnIfMissing bool
}

// Result is the plain-text extraction result.
type Result struct {
	Title      string
	PageCount  int
	Pages      []int
	Markdown   string
	TotalLines int
	TotalChars int
}

// Parser owns the lazy load state. It is safe for concurrent use.
type Parser struct {
	configured    string
	searchDirs    []string
	path          string
	warnIfMissing bool

	lib     *library
	loadErr error
	warned  sync.Once
}

// New creates a parser without touching the filesystem or loading the library.
func New(cfg Config) *Parser {
	return &Parser{
		configured:    cfg.Path,
		searchDirs:    append([]string(nil), cfg.SearchDirs...),
		path:          ResolvePath(cfg.Path, cfg.SearchDirs),
		warnIfMissing: cfg.WarnIfMissing,
	}
}

// Path reports the resolved library path (useful for startup logs and tests).
func (p *Parser) Path() string { return p.path }

// ResolvePath picks the first existing candidate from searchDirs. A relative
// path that does not exist is returned joined to the first search dir, which
// gives a useful error path and allows a later file drop to be picked up.
func ResolvePath(configured string, searchDirs []string) string {
	name := strings.TrimSpace(configured)
	if name == "" {
		name = defaultLibraryName()
	}
	if filepath.IsAbs(name) {
		return name
	}
	seen := make(map[string]bool, len(searchDirs))
	for _, dir := range searchDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if len(searchDirs) > 0 && strings.TrimSpace(searchDirs[0]) != "" {
		return filepath.Join(searchDirs[0], name)
	}
	return name
}

func defaultLibraryName() string {
	switch runtime.GOOS {
	case "windows":
		return "pdfium.dll"
	case "darwin":
		return "libpdfium.dylib"
	default:
		return "libpdfium.so"
	}
}

// BundledSubdir returns the platform-specific directory used by the release
// archives, for example third_party/pdfium/win-x64. Empty means the platform
// has no bundled library naming convention.
func BundledSubdir() string {
	return bundledSubdir(runtime.GOOS, runtime.GOARCH)
}

func bundledSubdir(goos, goarch string) string {
	arch := ""
	switch goos {
	case "windows":
		switch goarch {
		case "amd64":
			arch = "win-x64"
		case "arm64":
			arch = "win-arm64"
		}
	case "linux":
		switch goarch {
		case "amd64":
			arch = "linux-x64"
		case "arm64":
			arch = "linux-arm64"
		}
	case "darwin":
		switch goarch {
		case "amd64":
			arch = "mac-x64"
		case "arm64":
			arch = "mac-arm64"
		}
	}
	if arch == "" {
		return ""
	}
	return filepath.Join("third_party", "pdfium", arch)
}

// SearchDirs expands base directories with the bundled third_party path for
// the current platform. Each base is searched first, so a library dropped next
// to the executable or config overrides the bundled copy.
func SearchDirs(baseDirs ...string) []string {
	dirs := make([]string, 0, len(baseDirs)*2)
	bundled := BundledSubdir()
	seen := make(map[string]bool, len(baseDirs)*2)
	for _, base := range baseDirs {
		base = strings.TrimSpace(base)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		dirs = append(dirs, base)
		if bundled != "" {
			sub := filepath.Join(base, bundled)
			if !seen[sub] {
				seen[sub] = true
				dirs = append(dirs, sub)
			}
		}
	}
	return dirs
}

// Extract opens a PDF from memory and returns selected pages as plain text.
// pages are 1-based; maxPages limits the default selection when pages is empty.
func (p *Parser) Extract(ctx context.Context, filePath string, pages []int, maxPages int) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	extractMu.Lock()
	defer extractMu.Unlock()

	lib, err := p.ensureLibrary()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("fetch.pdfium.read_failed"), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return lib.extract(ctx, data, filePath, pages, maxPages)
}

// ensureLibrary loads the library on first use. A missing file is not cached:
// dropping the DLL next to the executable later works without a restart.
func (p *Parser) ensureLibrary() (*library, error) {
	if p.lib != nil {
		return p.lib, nil
	}
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	// Re-resolve on every attempt while unloaded so a library dropped into any
	// search directory after startup is picked up without a restart.
	path := ResolvePath(p.configured, p.searchDirs)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s is a directory", path)
		}
		if p.warnIfMissing {
			p.warnWanted(func() {
				log.WarnT("log.fetch.webfetch.pdfium_unavailable", "path", path, "err", err)
			})
		}
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, path)
	}
	lib, err := loadPDFium(path)
	if err != nil {
		p.loadErr = err
		p.warnWanted(func() {
			log.WarnT("log.fetch.webfetch.pdfium_load_failed", "path", p.path, "err", err)
		})
		return nil, err
	}
	p.lib = lib
	return lib, nil
}

func (p *Parser) warnWanted(fn func()) {
	p.warned.Do(fn)
}

// library is the process-wide PDFium binding. PDFium is not fully thread-safe,
// so extractMu serializes every call; a single load covers all parsers.
type library struct {
	handle uintptr

	initLibrary  func()
	getLastError func() uint32

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
	libOnce   sync.Once
	libShared *library
	libErr    error
	extractMu sync.Mutex
)

func loadPDFium(path string) (*library, error) {
	libOnce.Do(func() {
		libShared, libErr = openPDFium(path)
	})
	return libShared, libErr
}

func openPDFium(path string) (*library, error) {
	handle, err := loadLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("fetch.pdfium.load_failed"), err)
	}
	lib := &library{handle: handle}
	if err := lib.bind(); err != nil {
		_ = unloadLibrary(handle)
		return nil, err
	}
	lib.initLibrary()
	return lib, nil
}

func (l *library) bind() (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%s: %v", i18n.T("fetch.pdfium.load_failed"), rec)
		}
	}()
	purego.RegisterLibFunc(&l.initLibrary, l.handle, "FPDF_InitLibrary")
	purego.RegisterLibFunc(&l.getLastError, l.handle, "FPDF_GetLastError")
	purego.RegisterLibFunc(&l.loadMemDocument, l.handle, "FPDF_LoadMemDocument")
	purego.RegisterLibFunc(&l.getPageCount, l.handle, "FPDF_GetPageCount")
	purego.RegisterLibFunc(&l.loadPage, l.handle, "FPDF_LoadPage")
	purego.RegisterLibFunc(&l.closePage, l.handle, "FPDF_ClosePage")
	purego.RegisterLibFunc(&l.closeDocument, l.handle, "FPDF_CloseDocument")
	purego.RegisterLibFunc(&l.textLoadPage, l.handle, "FPDFText_LoadPage")
	purego.RegisterLibFunc(&l.textClosePage, l.handle, "FPDFText_ClosePage")
	purego.RegisterLibFunc(&l.textCountChars, l.handle, "FPDFText_CountChars")
	purego.RegisterLibFunc(&l.textGetText, l.handle, "FPDFText_GetText")
	return nil
}

func (l *library) extract(ctx context.Context, data []byte, filePath string, pages []int, maxPages int) (*Result, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%s: empty file", i18n.T("fetch.pdfium.read_failed"))
	}
	doc := l.loadMemDocument(unsafe.Pointer(&data[0]), len(data), 0)
	if doc == 0 {
		return nil, fmt.Errorf("%s: %s", i18n.T("fetch.pdfium.load_failed"), lastErrorMessage(l.getLastError()))
	}
	defer l.closeDocument(doc)

	pageCount := l.getPageCount(doc)
	selected := selectPages(pageCount, pages, maxPages)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%s: %s", i18n.T("fetch.pdfium.no_pages"), i18n.T("fetch.pdfium.no_text"))
	}

	texts := make([]string, 0, len(selected))
	for _, pageNo := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page := l.loadPage(doc, pageNo-1)
		if page == 0 {
			continue
		}
		text := l.pageText(page)
		l.closePage(page)
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			texts = append(texts, trimmed)
		}
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoText, i18n.T("fetch.pdfium.no_text"))
	}

	markdown := cleanText(strings.Join(texts, "\n\n---\n\n"))
	if markdown == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoText, i18n.T("fetch.pdfium.no_text"))
	}
	return &Result{
		Title:      titleFromText(filePath, markdown),
		PageCount:  pageCount,
		Pages:      selected,
		Markdown:   markdown,
		TotalLines: strings.Count(markdown, "\n") + 1,
		TotalChars: len(markdown),
	}, nil
}

func (l *library) pageText(page uintptr) string {
	textPage := l.textLoadPage(page)
	if textPage == 0 {
		return ""
	}
	defer l.textClosePage(textPage)

	count := l.textCountChars(textPage)
	if count <= 0 {
		return ""
	}
	buf := make([]uint16, count+1)
	written := l.textGetText(textPage, 0, count+1, unsafe.Pointer(&buf[0]))
	if written <= 1 {
		return ""
	}
	if written > len(buf) {
		written = len(buf)
	}
	return decodeUTF16(buf[:written-1])
}

func decodeUTF16(buf []uint16) string {
	for i, v := range buf {
		if v == 0 {
			buf = buf[:i]
			break
		}
	}
	if len(buf) == 0 {
		return ""
	}
	return string(utf16.Decode(buf))
}

func lastErrorMessage(code uint32) string {
	switch code {
	case 0:
		return "no PDFium error code"
	case 2:
		return "file not found or could not be opened"
	case 3:
		return "not a PDF file or corrupted"
	case 4:
		return "password required or incorrect"
	case 5:
		return "unsupported security scheme"
	case 6:
		return "page not found or content error"
	default:
		return fmt.Sprintf("PDFium error %d", code)
	}
}

// selectPages mirrors the built-in parser: requested 1-based pages win,
// otherwise the first maxPages pages are used.
func selectPages(pageCount int, requested []int, maxPages int) []int {
	if pageCount <= 0 {
		return nil
	}
	if len(requested) > 0 {
		seen := make(map[int]bool, len(requested))
		out := make([]int, 0, len(requested))
		for _, page := range requested {
			if page < 1 || page > pageCount || seen[page] {
				continue
			}
			seen[page] = true
			out = append(out, page)
		}
		sort.Ints(out)
		return out
	}
	n := pageCount
	if maxPages > 0 && n > maxPages {
		n = maxPages
	}
	out := make([]int, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, i)
	}
	return out
}

// cleanText normalizes line endings and collapses runs of blank lines.
func cleanText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var b strings.Builder
	blank := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if !blank && b.Len() > 0 {
				b.WriteByte('\n')
			}
			blank = true
			continue
		}
		blank = false
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func titleFromText(filePath, text string) string {
	for _, line := range strings.SplitN(text, "\n", 5) {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "# ")
		if line != "" && len(line) < 200 {
			return line
		}
	}
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
