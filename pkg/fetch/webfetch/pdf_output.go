package webfetch

import (
	"strings"

	"websearch/pkg/i18n"

	"github.com/daidaiJ/go-webfetch/store"
)

// StoreLargePDFResult preserves remote text fallback's existing output limits.
func (f *Fetcher) StoreLargePDFResult(result *Result, sourceURL string) error {
	lines, size := strings.Count(result.Markdown, "\n")+1, len(result.Markdown)
	maxLines := f.maxInlineLines
	if maxLines <= 0 {
		maxLines = 100
	}
	if lines <= maxLines && size <= 100000 && (f.maxInlineChars <= 0 || size <= f.maxInlineChars) {
		return nil
	}
	files, err := store.NewFileStore(f.outputDir, f.fileTTL)
	if err != nil {
		return err
	}
	path, totalLines, totalChars, _, err := files.WriteMarkdown(result.Markdown, result.Title, sourceURL)
	if err != nil {
		return err
	}
	result.Mode, result.FilePath = "saved_to_file", path
	result.TotalLines, result.TotalChars = totalLines, totalChars
	result.Markdown = ""
	result.AgentHint = i18n.T("fetch.webfetch.agent_hint_read_file")
	return nil
}
