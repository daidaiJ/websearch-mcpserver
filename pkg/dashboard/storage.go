package dashboard

import (
	"context"
	"errors"
	"net/http"
	"os"

	"websearch/pkg/fetch"
	"websearch/pkg/i18n"
)

type storageEntry struct {
	Enabled bool   `json:"enabled"`
	Count   int64  `json:"count"`
	Bytes   int64  `json:"bytes"`
	Error   string `json:"error,omitempty"`
}

func (h *Handler) storageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	var stats struct {
		Cache   storageEntry `json:"cache"`
		History storageEntry `json:"history"`
		Fetch   struct {
			storageEntry
			Expired   int64 `json:"expired"`
			Clearable int64 `json:"clearable"`
		} `json:"fetchdata"`
	}
	if h.cache != nil {
		stats.Cache = databaseStorage(r.Context(), h.conf.GetCacheStoragePath(), h.cache.Count)
	}
	if h.history != nil {
		stats.History = databaseStorage(r.Context(), h.conf.GetSearchHistoryStoragePath(), h.history.Count)
	}
	cfg := h.conf.CleanFetch
	files, err := fetch.ManageTemporaryOutputs(r.Context(), cfg.OutputDir(), cfg.OutputTTL(), fetch.ReportTemporaryOutputs)
	stats.Fetch.storageEntry = storageEntry{Enabled: true, Count: files.Count, Bytes: files.Bytes}
	stats.Fetch.Expired, stats.Fetch.Clearable = files.Expired, files.Clearable
	if err != nil {
		stats.Fetch.Error = i18n.TCtx(r.Context(), "dashboard.storage.stats_failed")
	}
	writeJSON(w, http.StatusOK, stats)
}

func databaseStorage(ctx context.Context, path string, count func(context.Context) (int64, error)) storageEntry {
	entry := storageEntry{Enabled: true}
	rows, err := count(ctx)
	if err != nil {
		entry.Error = i18n.TCtx(ctx, "dashboard.storage.stats_failed")
		return entry
	}
	entry.Count = rows
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			entry.Error = i18n.TCtx(ctx, "dashboard.storage.stats_failed")
			return entry
		}
		entry.Bytes += info.Size()
	}
	return entry
}

func (h *Handler) clearHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.storage.confirm_required")})
		return
	}
	if h.history == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.storage.history_disabled")})
		return
	}
	count, err := h.history.Clear(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.storage.clear_failed")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": count})
}

func (h *Handler) clearTemporaryOutputs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Confirm bool   `json:"confirm"`
		Scope   string `json:"scope"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.storage.confirm_required")})
		return
	}
	var action fetch.TemporaryOutputAction
	switch req.Scope {
	case "expired":
		action = fetch.ClearExpiredTemporaryOutputs
	case "all":
		action = fetch.ClearAllTemporaryOutputs
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.storage.scope_required")})
		return
	}
	cfg := h.conf.CleanFetch
	stats, err := fetch.ManageTemporaryOutputs(r.Context(), cfg.OutputDir(), cfg.OutputTTL(), action)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.storage.files_clear_failed", "Count", stats.Cleared), "cleared": stats.Cleared})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": stats.Cleared})
}
