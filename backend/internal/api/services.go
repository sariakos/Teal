package api

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/compose"
	"github.com/sariakos/teal/backend/internal/store"
)

// servicesHandler exposes compose-parsed service lists for the per-app
// Routes UI. Reads the most-recently-deployed compose so the UI shows
// what's actually live; falls back to the stored ComposeFile (for
// paste-compose apps that haven't deployed yet).
type servicesHandler struct {
	logger      *slog.Logger
	store       *store.Store
	workdirRoot string
}

func (h *servicesHandler) list(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	yaml, source, err := loadEffectiveCompose(h.workdirRoot, app.Slug, app.ComposeFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load compose: "+err.Error())
		return
	}
	if yaml == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"services": []compose.ServiceInfo{},
			"source":   "none",
			"hint":     "no compose file yet — deploy at least once (git apps) or paste a compose (advanced apps) so Teal can list services",
		})
		return
	}
	services, err := compose.ListServices(yaml)
	if err != nil {
		writeError(w, http.StatusBadRequest, "compose parse: "+err.Error())
		return
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{
		"services": services,
		"source":   source,
	})
}

