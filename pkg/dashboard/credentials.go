package dashboard

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/privatefile"
)

var credentialsMu sync.Mutex

func (h *Handler) currentCredentialEditor() *config.CredentialEditor {
	if h.credentialEditor == nil {
		h.credentialEditor = config.NewCredentialEditor(h.conf)
	}
	return h.credentialEditor
}

func (h *Handler) credentialView() (config.CredentialView, error) {
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	editor := h.currentCredentialEditor()
	data, err := readCredentialFile(h.conf.GetDashboardSecretsPath())
	if err != nil {
		view, _ := editor.View([]byte{})
		return view, err
	}
	return editor.View(data)
}

func (h *Handler) secrets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		view, err := h.credentialView()
		if err != nil {
			writeCredentialError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPost:
		if !h.authorizeWrite(w, r) {
			return
		}
		var req config.CredentialEditRequest
		if err := decodeJSON(r, &req); err != nil {
			writeCredentialError(ctx, w, config.ErrCredentialRequest)
			return
		}
		result, err := h.editCredential(req)
		if err != nil {
			writeCredentialError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		methodNotAllowed(w)
	}
}

func writeCredentialError(ctx context.Context, w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var message string
	switch {
	case errors.Is(err, config.ErrCredentialConflict):
		status, message = http.StatusConflict, i18n.TCtx(ctx, "dashboard.credentials.conflict")
	case errors.Is(err, config.ErrSettingsPathsChanged):
		status, message = http.StatusConflict, i18n.TCtx(ctx, "dashboard.settings.paths_changed")
	case errors.Is(err, config.ErrCredentialRequest):
		status, message = http.StatusBadRequest, i18n.TCtx(ctx, "dashboard.credentials.invalid_request")
	case errors.Is(err, config.ErrCredentialLabel):
		status, message = http.StatusBadRequest, i18n.TCtx(ctx, "dashboard.credentials.invalid_label")
	default:
		message = i18n.TCtx(ctx, "dashboard.credentials.file_unavailable")
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func (h *Handler) editCredential(req config.CredentialEditRequest) (config.CredentialEditResult, error) {
	if err := h.validateCredentialPaths(); err != nil {
		return config.CredentialEditResult{}, err
	}
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	path := h.conf.GetDashboardSecretsPath()
	data, err := readCredentialFile(path)
	if err != nil {
		return config.CredentialEditResult{}, err
	}
	editor := h.currentCredentialEditor()
	result, next, err := editor.Edit(data, req)
	if err != nil || next == nil {
		return result, err
	}
	result.Backup, err = replaceCredentialFile(path, data, next)
	if err == nil {
		result.Revision = editor.Observe(next)
	}
	return result, err
}

func (h *Handler) validateCredentialPaths() error {
	paths := h.conf.SettingsFilePaths()
	if paths.Overlay == "" {
		return nil
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	docs, err := readSettingsDocuments(config.GetConfigFile(), paths.Overlay)
	if err != nil {
		return err
	}
	_, err = decodeSettingsAtPaths(docs.main.data, docs.overlay.data, paths)
	return err
}

func readCredentialFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, config.ErrCredentialFile
	}
	return os.ReadFile(path)
}

func writePrivateCredentialFile(path string, data []byte) error {
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
	check, err := readCredentialFile(path)
	if err != nil || !bytes.Equal(check, data) {
		_ = os.Remove(path)
		return config.ErrCredentialFile
	}
	return nil
}

func replaceCredentialFile(path string, old, next []byte) (bool, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	temp := filepath.Join(dir, ".dashboard-secrets-"+rand.Text()+".tmp")
	if err := writePrivateCredentialFile(temp, next); err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(temp) }()
	if old != nil {
		backupDir := filepath.Join(dir, "dashboard-secrets-backups")
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return false, err
		}
		name := "secrets-" + time.Now().UTC().Format(settingsBackupStamp) + "-" + rand.Text() + ".json"
		if err := writePrivateCredentialFile(filepath.Join(backupDir, name), old); err != nil {
			return false, err
		}
	}
	latest, err := readCredentialFile(path)
	if err != nil || !bytes.Equal(latest, old) || (latest == nil) != (old == nil) {
		return false, config.ErrCredentialConflict
	}
	if err := os.Rename(temp, path); err != nil {
		return false, err
	}
	return old != nil, nil
}
