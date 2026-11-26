package fetch

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const SearchOutputTTL = 7 * 24 * time.Hour

// Empty the disposable outputs without touching files that may still be in use by a writer.
const temporaryOutputWriteWindow = time.Minute

var (
	searchOutputName = regexp.MustCompile(`^search-\d{8}_\d{6}-[0-9a-f]{8}\.md$`)
	webOutputName    = regexp.MustCompile(`^\d{8}_\d{6}_[^_]+_[0-9a-f]{6}\.md$`)
)

func IsSearchOutputName(name string) bool { return searchOutputName.MatchString(name) }
func IsWebOutputName(name string) bool    { return webOutputName.MatchString(name) }

type TemporaryOutputAction int

const (
	ReportTemporaryOutputs TemporaryOutputAction = iota
	ClearExpiredTemporaryOutputs
	ClearAllTemporaryOutputs
)

type TemporaryOutputStats struct {
	Count     int64
	Bytes     int64
	Expired   int64
	Clearable int64
	Cleared   int64
}

// ManageTemporaryOutputs only visits regular files directly inside the configured output root.
func ManageTemporaryOutputs(ctx context.Context, dir string, webTTL time.Duration, action TemporaryOutputAction) (TemporaryOutputStats, error) {
	var stats TemporaryOutputStats
	root, err := openTemporaryOutputRoot(dir)
	if errors.Is(err, os.ErrNotExist) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	defer root.Close()
	list, err := root.Open(".")
	if err != nil {
		return stats, err
	}
	defer list.Close()

	now := time.Now()
	var firstErr error
	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		entries, listErr := list.ReadDir(128)
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			name := entry.Name()
			ttl := webTTL
			switch {
			case IsSearchOutputName(name):
				ttl = SearchOutputTTL
			case IsWebOutputName(name):
			default:
				continue
			}
			info, err := root.Lstat(name)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			stats.Count++
			stats.Bytes += info.Size()
			age := now.Sub(info.ModTime())
			if age < temporaryOutputWriteWindow {
				continue
			}
			stats.Clearable++
			if age >= ttl {
				stats.Expired++
			}
			if action == ClearAllTemporaryOutputs || (action == ClearExpiredTemporaryOutputs && age >= ttl) {
				if err := root.Remove(name); err == nil {
					stats.Cleared++
				} else if !errors.Is(err, os.ErrNotExist) && firstErr == nil {
					firstErr = err
				}
			}
		}
		if listErr == io.EOF {
			return stats, firstErr
		}
		if listErr != nil {
			return stats, listErr
		}
	}
}

func openTemporaryOutputRoot(dir string) (*os.Root, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// Check every component because a real directory inside a junction still
	// resolves outside the configured tree. Compare identities after opening it.
	var ancestors []string
	for ancestor := abs; ; ancestor = filepath.Dir(ancestor) {
		ancestors = append(ancestors, ancestor)
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	var expected os.FileInfo
	for i := len(ancestors) - 1; i >= 0; i-- {
		ancestor := ancestors[i]
		info, err := os.Lstat(ancestor)
		if err != nil {
			return nil, err
		}
		if info.Mode().Type() != os.ModeDir {
			return nil, os.ErrInvalid
		}
		if ancestor == abs {
			expected = info
		}
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	actual, err := root.Stat(".")
	if err == nil && os.SameFile(expected, actual) {
		return root, nil
	}
	root.Close()
	if err != nil {
		return nil, err
	}
	return nil, os.ErrInvalid
}
