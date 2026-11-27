package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/privatefile"
)

func configRevision(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func backupDirectory(c config.Config) string {
	return filepath.Join(filepath.Dir(c.GetDashboardStoragePath()), "dashboard-backups")
}

func overlayBackupDirectory(c config.Config) string {
	return filepath.Join(filepath.Dir(c.GetDashboardStoragePath()), "dashboard-overlay-backups")
}

type settingsFile struct {
	path string
	data []byte
	info os.FileInfo
}

type settingsDocuments struct {
	main, overlay settingsFile
}

func readSettingsFile(path string) (settingsFile, error) {
	file := settingsFile{path: path}
	info, err := os.Stat(path)
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() {
		return file, errSettingsUnreadable
	}
	file.data, err = os.ReadFile(path)
	if err != nil {
		return file, err
	}
	file.info = info
	return file, nil
}

func readSettingsDocuments(path, overlayPath string) (settingsDocuments, error) {
	var docs settingsDocuments
	var err error
	docs.main, err = readSettingsFile(path)
	if err != nil {
		return docs, errSettingsUnreadable
	}
	if overlayPath == "" {
		return docs, nil
	}
	docs.overlay, err = readSettingsFile(overlayPath)
	if errors.Is(err, os.ErrNotExist) {
		return docs, nil
	}
	if err != nil || os.SameFile(docs.main.info, docs.overlay.info) {
		return docs, errSettingsUnreadable
	}
	return docs, nil
}

func (d settingsDocuments) revision() string {
	main := configRevision(d.main.data)
	if d.overlay.info == nil {
		return main
	}
	// Each digest has a fixed length, so the two files have independent boundaries.
	return configRevision([]byte(main + configRevision(d.overlay.data)))
}

func (d settingsDocuments) fieldFile(path string) string {
	if d.overlay.info != nil && strings.HasPrefix(path, "dashboard.") {
		return "dashboard.yaml"
	}
	return "config.yaml"
}

const settingsBackupStamp = "20060102-150405.000000000"

func backupFiles(name string) (main, overlay bool) {
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".yaml") {
		return false, false
	}
	prefix, stamp, found := strings.Cut(strings.TrimSuffix(name, ".yaml"), "-")
	if !found {
		return false, false
	}
	if _, err := time.Parse(settingsBackupStamp, stamp); err != nil {
		return false, false
	}
	return prefix == "config" || prefix == "settings", prefix == "dashboard" || prefix == "settings"
}

func backupFileInfo(dir, name string) (os.FileInfo, bool) {
	info, err := os.Lstat(filepath.Join(dir, name))
	return info, err == nil && info.Mode().IsRegular()
}

func settingsBackups(c config.Config) []map[string]string {
	backups := []map[string]string{}
	appendBackups := func(dir string, isOverlay bool) {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			name := entry.Name()
			main, overlay := backupFiles(name)
			if (!isOverlay && !main) || (isOverlay && (main || !overlay)) {
				continue
			}
			info, regular := backupFileInfo(dir, name)
			if !regular {
				continue
			}
			if main && overlay {
				if _, present := backupFileInfo(overlayBackupDirectory(c), name); !present {
					continue
				}
			}
			backups = append(backups, map[string]string{"name": name, "created_at": info.ModTime().Format(time.RFC3339)})
		}
	}
	appendBackups(backupDirectory(c), false)
	appendBackups(overlayBackupDirectory(c), true)
	sort.Slice(backups, func(i, j int) bool {
		_, left, _ := strings.Cut(backups[i]["name"], "-")
		_, right, _ := strings.Cut(backups[j]["name"], "-")
		return left > right
	})
	return backups
}

func readBackup(dir, name string, loc *i18n.Localizer) ([]byte, error) {
	if _, regular := backupFileInfo(dir, name); !regular {
		return nil, errors.New(loc.T("dashboard.settings.backup_unavailable"))
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, errors.New(loc.T("dashboard.settings.backup_unavailable"))
	}
	return data, nil
}

type settingsEdit struct {
	settingsFile
	backupDir string
	target    []byte
	handle    *os.File
	observed  []byte
	expected  *settingsFile
}

func (edit *settingsEdit) prepare() error {
	file, err := os.OpenFile(edit.path, os.O_RDWR, 0)
	if err != nil {
		return errSettingsUnreadable
	}
	edit.handle = file
	info, err := file.Stat()
	if err != nil || !os.SameFile(info, edit.info) {
		return errSettingsConflict
	}
	return nil
}

func writeSettingsContent(file *os.File, before, after []byte) ([]byte, error) {
	written, err := file.WriteAt(after, 0)
	if err == nil {
		err = file.Truncate(int64(len(after)))
		if err == nil {
			return after, file.Sync()
		}
	}
	// WriteAt can fail after a partial write; truncate can fail after a full
	// write. Record the expected bytes so rollback can detect external edits.
	if written < len(before) {
		partial := bytes.Clone(before)
		copy(partial, after[:written])
		return partial, err
	}
	return after[:written], err
}

func writeSettingsBackup(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	file, err := privatefile.OpenNew(path)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(path)
		return err
	}
	check, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(check, data) {
		_ = os.Remove(path)
		return errors.New("config backup readback mismatch")
	}
	return nil
}

func settingsDocumentsMatch(actual, expected settingsDocuments) bool {
	match := func(a, b settingsFile) bool {
		if a.info == nil || b.info == nil {
			return a.info == nil && b.info == nil
		}
		return os.SameFile(a.info, b.info) && bytes.Equal(a.data, b.data)
	}
	return match(actual.main, expected.main) && match(actual.overlay, expected.overlay)
}

func updateSettingsFiles(c config.Config, original, next settingsDocuments, clearCache func() error, loc *i18n.Localizer) (string, error) {
	var edits []settingsEdit
	expected := original
	prefix := "config-"
	if !bytes.Equal(original.main.data, next.main.data) {
		edits = append(edits, settingsEdit{settingsFile: original.main, target: next.main.data, backupDir: backupDirectory(c), expected: &expected.main})
	}
	if !bytes.Equal(original.overlay.data, next.overlay.data) {
		prefix = "dashboard-"
		if len(edits) != 0 {
			prefix = "settings-"
		}
		edits = append(edits, settingsEdit{settingsFile: original.overlay, target: next.overlay.data, backupDir: overlayBackupDirectory(c), expected: &expected.overlay})
	}
	if len(edits) == 0 {
		return "", nil
	}
	for i := range edits {
		edit := &edits[i]
		err := edit.prepare()
		if edit.handle != nil {
			defer func() { _ = edit.handle.Close() }()
		}
		if err != nil {
			return "", err
		}
	}
	name := prefix + time.Now().UTC().Format(settingsBackupStamp) + ".yaml"
	for i, edit := range edits {
		if err := writeSettingsBackup(edit.backupDir, name, edit.data); err != nil {
			for _, saved := range edits[:i] {
				_ = os.Remove(filepath.Join(saved.backupDir, name))
			}
			return "", errors.New(loc.T("dashboard.settings.backup_failed"))
		}
	}

	touched := 0
	rollback := func(cause error) (string, error) {
		failed := false
		for i := touched - 1; i >= 0; i-- {
			edit := &edits[i]
			current, err := readSettingsFile(edit.path)
			if err == nil && os.SameFile(current.info, edit.info) && bytes.Equal(current.data, edit.data) {
				continue
			}
			if err != nil || !os.SameFile(current.info, edit.info) || !bytes.Equal(current.data, edit.observed) {
				// The file no longer contains this transaction's write. Leave an
				// external change intact and retain the backup for manual recovery.
				failed = true
				continue
			}
			if _, err := writeSettingsContent(edit.handle, current.data, edit.data); err != nil {
				failed = true
				continue
			}
			check, err := readSettingsFile(edit.path)
			if err != nil || !os.SameFile(check.info, edit.info) || !bytes.Equal(check.data, edit.data) {
				failed = true
			}
		}
		if failed {
			return "", errors.New(loc.T("dashboard.settings.rollback_failed", "reason", settingsErrorMessage(loc, cause), "backup", name))
		}
		for _, edit := range edits {
			_ = os.Remove(filepath.Join(edit.backupDir, name))
		}
		return "", cause
	}

	// Work through handles opened before either write: individual files may be Docker
	// bind mounts. Compare both files immediately before each write and after readback.
	for i := range edits {
		latest, err := readSettingsDocuments(original.main.path, original.overlay.path)
		if err != nil || !settingsDocumentsMatch(latest, expected) {
			return rollback(errSettingsConflict)
		}
		edit := &edits[i]
		touched++
		edit.observed, err = writeSettingsContent(edit.handle, edit.data, edit.target)
		if err != nil {
			return rollback(errors.New(loc.T("dashboard.settings.write_failed")))
		}
		edit.expected.data = edit.target
	}
	check, err := readSettingsDocuments(original.main.path, original.overlay.path)
	if err != nil || !settingsDocumentsMatch(check, next) {
		return rollback(errors.New(loc.T("dashboard.settings.readback_failed")))
	}
	if clearCache != nil {
		if err := clearCache(); err != nil {
			return rollback(errors.New(loc.T("dashboard.settings.cache_clear_failed")))
		}
	}
	return name, nil
}
