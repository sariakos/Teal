package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// sharedEnvVarsHandler exposes admin-only CRUD for shared env vars and
// member-accessible allow-list configuration for individual apps.
type sharedEnvVarsHandler struct {
	logger *slog.Logger
	store  *store.Store
	codec  *crypto.Codec
}

// listShared returns every shared env var. Values masked unless reveal=true.
// Admin-gated by the router.
func (h *sharedEnvVarsHandler) listShared(w http.ResponseWriter, r *http.Request) {
	reveal := r.URL.Query().Get("reveal") == "true"
	rows, err := h.store.EnvVars.ListShared(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list shared env vars")
		return
	}
	helper := envVarsHandler{logger: h.logger, store: h.store, codec: h.codec}
	out, err := helper.serialize(rows, reveal, envvarSharedOpener(h.codec))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "decrypt: "+err.Error())
		return
	}
	if reveal {
		recordAudit(r.Context(), h.logger, h.store.AuditLogs,
			domain.AuditActionEnvVarReveal, "shared_envvar", "",
			clientIP(r), fmt.Sprintf("revealed %d shared env var(s)", len(rows)), "")
	}
	writeJSON(w, http.StatusOK, out)
}

// upsertShared inserts or updates a shared env var. Admin-gated.
func (h *sharedEnvVarsHandler) upsertShared(w http.ResponseWriter, r *http.Request) {
	var req upsertEnvVarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if !envVarKeyRe.MatchString(req.Key) {
		writeError(w, http.StatusBadRequest, "key must match [A-Za-z_][A-Za-z0-9_]*")
		return
	}
	if strings.ContainsAny(req.Value, "\r\n") {
		writeError(w, http.StatusBadRequest, "value must not contain CR/LF")
		return
	}
	ciphertext, err := h.codec.Seal(domain.CodecPurposeEnvVarShared, domain.EnvVarSharedAAD(req.Key), []byte(req.Value))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encrypt: "+err.Error())
		return
	}
	if _, err := h.store.EnvVars.UpsertShared(r.Context(), req.Key, ciphertext); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionSharedEnvVarUpdate, "shared_envvar", req.Key,
		clientIP(r), "set shared env "+req.Key, "")
	w.WriteHeader(http.StatusNoContent)
}

// deleteShared removes a shared env var by key. App allow-lists are left
// intact — the engine logs a warning when an opted-in key has no shared
// row. Admin-gated.
func (h *sharedEnvVarsHandler) deleteShared(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if !envVarKeyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "invalid key")
		return
	}
	if err := h.store.EnvVars.DeleteShared(r.Context(), key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "shared env not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "delete: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionSharedEnvVarDelete, "shared_envvar", key,
		clientIP(r), "deleted shared env "+key, "")
	w.WriteHeader(http.StatusNoContent)
}

// appSharedListResponse is what GET /apps/{slug}/shared-envvars returns:
// the keys this App has opted in to plus the list of available shared keys
// so the UI can render a single-select dropdown without an extra round-trip.
type appSharedListResponse struct {
	Included []string `json:"included"`
	Available []string `json:"available"`
}

// listAppShared returns the App's shared allow-list and the platform-wide
// available keys. Member-accessible.
func (h *sharedEnvVarsHandler) listAppShared(w http.ResponseWriter, r *http.Request) {
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
	included, err := h.store.AppSharedEnvVars.ListForApp(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list allow-list: "+err.Error())
		return
	}
	availableRows, err := h.store.EnvVars.ListShared(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list shared: "+err.Error())
		return
	}
	available := make([]string, 0, len(availableRows))
	for _, v := range availableRows {
		available = append(available, v.Key)
	}
	if included == nil {
		included = []string{}
	}
	writeJSON(w, http.StatusOK, appSharedListResponse{Included: included, Available: available})
}

type setAppSharedRequest struct {
	Keys []string `json:"keys"`
}

// setAppShared overwrites the App's shared allow-list. The body's keys
// list IS the new state — anything not present is removed.
// Member-accessible.
func (h *sharedEnvVarsHandler) setAppShared(w http.ResponseWriter, r *http.Request) {
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
	var req setAppSharedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	for _, k := range req.Keys {
		if !envVarKeyRe.MatchString(k) {
			writeError(w, http.StatusBadRequest, "invalid key in list: "+k)
			return
		}
	}
	if err := h.store.AppSharedEnvVars.Set(r.Context(), app.ID, req.Keys); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppSharedEnvSet, "app", strings.TrimSpace(slug),
		clientIP(r), fmt.Sprintf("set shared allow-list (%d keys)", len(req.Keys)), "")
	w.WriteHeader(http.StatusNoContent)
}
