//go:build !windows

// Package privatefile creates files for private configuration and backups.
package privatefile

import "os"

// OpenNew exclusively creates a file restricted to its owner.
func OpenNew(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}
