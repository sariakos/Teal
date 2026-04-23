package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

type apiKeyResponse struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func apiKeyToResponse(k domain.APIKey) apiKeyResponse {
	return apiKeyResponse{
		ID: k.ID, Name: k.Name, LastUsedAt: k.LastUsedAt,
		RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt,
	}
}

type apiKeyCreateRequest struct {
	Name string `json:"name"`
}

// apiKeyCreateResponse extends the standard shape with the raw key — shown
// once, never again. The frontend must surface this prominently.
type apiKeyCreateResponse struct {
	apiKeyResponse
	Key string `json:"key"`
}

type apiKeysHandler struct {
	logger *slog.Logger
	store  *store.Store
	mgr    *auth.APIKeyManager
}

// list returns the calling user's API keys.
func (h *apiKeysHandler) list(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	rows, err := h.store.APIKeys.ListForUser(r.Context(), subj.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	out := make([]apiKeyResponse, 0, len(rows))
	for _, k := range rows {
		out = append(out, apiKeyToResponse(k))
	}
	writeJSON(w, http.StatusOK, out)
}

// create issues a new API key for the calling user. The raw key is included
// in the response and never reproducible.
func (h *apiKeysHandler) create(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	var req apiKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	raw, key, err := h.mgr.Generate(r.Context(), subj.UserID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create key")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserUpdate, "apikey", strconv.FormatInt(key.ID, 10),
		clientIP(r), "issued api key "+req.Name, "")

	writeJSON(w, http.StatusCreated, apiKeyCreateResponse{
		apiKeyResponse: apiKeyToResponse(key),
		Key:            raw,
	})
}

// revoke marks the key as revoked. The row remains for audit-log linkage.
func (h *apiKeysHandler) revoke(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	key, err := h.store.APIKeys.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A user can only revoke their own keys; admins can revoke anyone's via
	// a separate admin endpoint (not in Phase 2 scope).
	if key.UserID != subj.UserID {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	if err := h.store.APIKeys.Revoke(r.Context(), id, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusConflict, "already revoked")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserUpdate, "apikey", strconv.FormatInt(id, 10),
		clientIP(r), "revoked api key", "")
	w.WriteHeader(http.StatusNoContent)
}
