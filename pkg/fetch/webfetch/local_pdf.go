package webfetch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"websearch/pkg/fetch/mineru"
	"websearch/pkg/i18n"
)

func (f *Fetcher) CanUseMinerULocalPDF() bool { return f.mineru != nil && f.mineru.HasToken() }

// ParseLocalPDFArchive is the explicit archival route, separate from text fallback.
func (f *Fetcher) ParseLocalPDFArchive(ctx context.Context, path, output string, pages []int) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()
	if !f.CanUseMinerULocalPDF() {
		return nil, errors.New(i18n.T("fetch.webfetch.mineru_token_required"))
	}
	result, err := f.mineru.ParseLocalArchive(ctx, localPathFromFileURL(path), output, pages, f.mineruPageBatch, f.mineruPageBudget)
	if result == nil {
		return nil, err
	}
	formatted, formatErr := localArchiveResult(result)
	return formatted, errors.Join(err, formatErr)
}

func localArchiveResult(local *mineru.LocalResult) (*Result, error) {
	file, err := os.Open(local.Markdown)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	chars, lines := 0, 1
	reader := bufio.NewReader(file)
	for {
		r, _, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		chars++
		if r == '\n' {
			lines++
		}
	}
	preamble := i18n.T("fetch.webfetch.local_package", "Package", local.Directory,
		"Source", local.Source, "Pages", mineru.LocalPageRanges(local.Pages), "Total", local.TotalPages, "Reused", local.Reused)
	if len(local.Remaining) > 0 {
		preamble += "\n" + i18n.T("fetch.webfetch.local_remaining", "Pages", mineru.LocalPageRanges(local.Remaining), "Package", local.Directory)
	}
	return &Result{Title: filepath.Base(local.Source), Mode: "saved_to_file", FilePath: local.Markdown,
		TotalChars: chars, TotalLines: lines, PageCount: local.TotalPages, ParsedPages: len(local.Pages),
		ParseEngine: "mineru-local", Preamble: preamble, Persistent: true}, nil
}
