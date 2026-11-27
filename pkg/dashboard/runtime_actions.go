package dashboard

import (
	"context"
	"net/http"
	"strings"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/i18n"
	"websearch/pkg/quota"
)

func (h *Handler) releaseCooldown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Engine  string `json:"engine"`
		Confirm bool   `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.runtime.confirm_required")})
		return
	}
	engine := strings.TrimSpace(req.Engine)
	if engine == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.runtime.engine_required")})
		return
	}
	if engine == "all" {
		engine = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": antirobot.ReleaseRouteCooldown(engine)})
}

func (h *Handler) refreshQuotas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Confirm  bool    `json:"confirm"`
		Provider *string `json:"provider"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.runtime.confirm_required")})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var items []quota.Item
	var refreshed bool
	if req.Provider == nil {
		items, refreshed = h.quota.Refresh(ctx)
	} else {
		item, refreshedProvider, err := h.quota.RefreshProvider(ctx, *req.Provider)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.runtime.quota_provider_unsupported")})
			return
		}
		items, refreshed = []quota.Item{item}, refreshedProvider
	}
	if ctx.Err() != nil {
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": i18n.TCtx(r.Context(), "dashboard.runtime.quota_timeout")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "refreshed": refreshed})
}
