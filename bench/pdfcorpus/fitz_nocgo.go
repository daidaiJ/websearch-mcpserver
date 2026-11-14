//go:build !cgo

package main

import "fmt"

const cgoEnabled = false

func extractMuPDF(string) (string, error) {
	return "", fmt.Errorf("go-fitz requires cgo")
}
