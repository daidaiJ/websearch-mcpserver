//go:build !windows

package privatefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	f, err := OpenNew(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("private file readable by other users")
	}
	if second, err := OpenNew(path); !os.IsExist(err) {
		if err == nil {
			second.Close()
		}
		t.Fatal("existing path not rejected")
	}
}
