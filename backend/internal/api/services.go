package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"

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

	yaml, source, err := h.loadCompose(app.Slug, app.ComposeFile)
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

// loadCompose returns the raw compose YAML to parse, plus a tag the UI
// surfaces ("checkout"/"stored"/"none"). Tries the most recent deploy's
// checkout first (fresh from the user's repo), then falls back to the
// stored ComposeFile.
func (h *servicesHandler) loadCompose(slug, storedYAML string) (string, string, error) {
	if h.workdirRoot != "" {
		dir := filepath.Join(h.workdirRoot, "deploys", slug)
		entries, err := os.ReadDir(dir)
		if err == nil {
			// Find the highest-numbered deployment dir whose
			// checkout/<git_compose_path-or-default> exists.
			ids := make([]int, 0, len(entries))
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if id, err := strconv.Atoi(e.Name()); err == nil {
					ids = append(ids, id)
				}
			}
			sort.Sort(sort.Reverse(sort.IntSlice(ids)))
			for _, id := range ids {
				// Try the typical compose locations the engine writes/reads.
				candidates := []string{
					filepath.Join(dir, strconv.Itoa(id), "checkout", "docker-compose.yml"),
					filepath.Join(dir, strconv.Itoa(id), "checkout", "compose.yml"),
				}
				for _, c := range candidates {
					if data, err := os.ReadFile(c); err == nil {
						return string(data), "checkout", nil
					}
				}
			}
		}
	}
	if storedYAML != "" {
		return storedYAML, "stored", nil
	}
	return "", "none", nil
}
