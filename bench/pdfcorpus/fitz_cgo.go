//go:build cgo

package main

import (
	"fmt"
	"strings"

	"github.com/gen2brain/go-fitz"
)

const cgoEnabled = true

func extractMuPDF(path string) (string, error) {
	doc, err := fitz.New(path)
	if err != nil {
		return "", err
	}
	defer doc.Close()
	var b strings.Builder
	for i := 0; i < doc.NumPage(); i++ {
		text, err := doc.Text(i)
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
