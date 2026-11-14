package main

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// pdfiumExtractor is the bench-facing surface shared by the dynamic-library
// backend and the WebAssembly backend. Both must produce the same plain text:
// one line per PDF text run, page order preserved.
type pdfiumExtractor interface {
	Extract(data []byte) (string, error)
	Close()
}

func joinPageText(pages []string) (string, error) {
	var b strings.Builder
	for _, text := range pages {
		if text == "" {
			continue
		}
		b.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("no text")
	}
	return b.String(), nil
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
