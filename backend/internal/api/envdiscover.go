package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/compose"
	"github.com/sariakos/teal/backend/internal/store"
)

// requiredEnvVarHandler exposes "what env vars does this app's compose
// project need?" so the UI can prompt the user to set them. Read-only;
// mutation goes through the existing /envvars + /shared-envvars
// endpoints.
type requiredEnvVarHandler struct {
	logger      *slog.Logger
	store       *store.Store
	workdirRoot string
}

// requiredEnvVarStatus narrows the discovered var into one of four
// states the UI renders distinctly.
type requiredEnvVarStatus string

const (
	envStatusSet      requiredEnvVarStatus = "set"      // per-app value present
	envStatusShared   requiredEnvVarStatus = "shared"   // app opted into a shared var with this key
	envStatusDefault  requiredEnvVarStatus = "default"  // unset but compose has ${VAR:-default}
	envStatusMissing  requiredEnvVarStatus = "missing"  // unset and no default → deploy will likely break
	envStatusUnclaimed requiredEnvVarStatus = "unclaimed" // shared key exists but app hasn't opted in
)

type requiredEnvVarResponse struct {
	Name         string               `json:"name"`
	Status       requiredEnvVarStatus `json:"status"`
	HasDefault   bool                 `json:"hasDefault"`
	DefaultValue string               `json:"defaultValue,omitempty"`
	Sources      []string             `json:"sources"`
}

type requiredEnvVarsListResponse struct {
	Vars   []requiredEnvVarResponse `json:"vars"`
	Source string                   `json:"source"` // "checkout" / "stored" / "none"
	Hint   string                   `json:"hint,omitempty"`
}

// list handles GET /apps/{slug}/required-envvars.
func (h *requiredEnvVarHandler) list(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup app")
		return
	}

	yaml, source, err := loadEffectiveCompose(h.workdirRoot, app.Slug, app.ComposeFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load compose: "+err.Error())
		return
	}
	if yaml == "" {
		writeJSON(w, http.StatusOK, requiredEnvVarsListResponse{
			Vars:   []requiredEnvVarResponse{},
			Source: "none",
			Hint:   "No compose available yet — deploy at least once (git apps) or paste a compose (advanced apps) so Teal can scan for env-var references.",
		})
		return
	}

	refs, err := compose.DiscoverEnvVars(yaml)
	if err != nil {
		writeError(w, http.StatusBadRequest, "compose parse: "+err.Error())
		return
	}

	// Build the set of currently-known keys: per-app vars (the
	// authoritative set), shared vars (only if the app opted in).
	appVars, err := h.store.EnvVars.ListForApp(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list app env: "+err.Error())
		return
	}
	appKeys := map[string]struct{}{}
	for _, v := range appVars {
		appKeys[v.Key] = struct{}{}
	}

	sharedKeys, err := h.store.AppSharedEnvVars.ListForApp(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list shared opt-ins: "+err.Error())
		return
	}
	sharedClaimed := map[string]struct{}{}
	for _, k := range sharedKeys {
		sharedClaimed[k] = struct{}{}
	}

	allShared, err := h.store.EnvVars.ListShared(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list shared: "+err.Error())
		return
	}
	sharedAvailable := map[string]struct{}{}
	for _, v := range allShared {
		sharedAvailable[v.Key] = struct{}{}
	}

	out := make([]requiredEnvVarResponse, 0, len(refs))
	for _, ref := range refs {
		status := envStatusMissing
		switch {
		case has(appKeys, ref.Name):
			status = envStatusSet
		case has(sharedClaimed, ref.Name):
			status = envStatusShared
		case ref.HasDefault:
			status = envStatusDefault
		case has(sharedAvailable, ref.Name):
			// A shared var with this name exists — the user just
			// hasn't opted in. Surface this so they can one-click
			// claim it.
			status = envStatusUnclaimed
		}
		out = append(out, requiredEnvVarResponse{
			Name:         ref.Name,
			Status:       status,
			HasDefault:   ref.HasDefault,
			DefaultValue: ref.DefaultValue,
			Sources:      ref.Sources,
		})
	}

	writeJSON(w, http.StatusOK, requiredEnvVarsListResponse{
		Vars:   out,
		Source: source,
	})
}

func has(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}
