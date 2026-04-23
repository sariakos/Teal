package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// envVarKeyRe restricts env-var keys to the POSIX-shell-safe alphabet:
// upper/lower letters, digits, underscore. Must start with a non-digit.
// Compose accepts more, but reading anything outside this set in shell is
// painful and we don't want to ship surprises. Validate at the API edge so
// the store and engine stay simple.
var envVarKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envVarResponse is the wire shape. Value is masked unless reveal=true was
// passed (and audited). HasValue is always present so the UI can show "(set)"
// even when masked.
type envVarResponse struct {
	Key       string    `json:"key"`
	Value     string    `json:"value,omitempty"` // empty when masked
	Masked    bool      `json:"masked"`
	HasValue  bool      `json:"hasValue"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type envVarsHandler struct {
	logger *slog.Logger
	store  *store.Store
	codec  *crypto.Codec
}

// listAppEnvVars returns the per-app env vars for an App. Values are masked
// by default; ?reveal=true decrypts and returns plaintext (audited).
func (h *envVarsHandler) listApp(w http.ResponseWriter, r *http.Request) {
	app, ok := h.lookupApp(w, r)
	if !ok {
		return
	}
	reveal := r.URL.Query().Get("reveal") == "true"
	rows, err := h.store.EnvVars.ListForApp(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list env vars")
		return
	}
	out, err := h.serialize(rows, reveal, envvarAppOpener(app.ID, h.codec))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "decrypt env: "+err.Error())
		return
	}
	if reveal {
		recordAudit(r.Context(), h.logger, h.store.AuditLogs,
			domain.AuditActionEnvVarReveal, "app", strconv.FormatInt(app.ID, 10),
			clientIP(r), fmt.Sprintf("revealed %d app env var(s)", len(rows)), "")
	}
	writeJSON(w, http.StatusOK, out)
}

type upsertEnvVarRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// upsertApp creates or replaces a single per-app env var.
func (h *envVarsHandler) upsertApp(w http.ResponseWriter, r *http.Request) {
	app, ok := h.lookupApp(w, r)
	if !ok {
		return
	}
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
	ciphertext, err := h.codec.Seal(domain.CodecPurposeEnvVarApp, domain.EnvVarAppAAD(app.ID, req.Key), []byte(req.Value))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encrypt: "+err.Error())
		return
	}
	if _, err := h.store.EnvVars.Upsert(r.Context(), app.ID, req.Key, ciphertext); err != nil {
		writeError(w, http.StatusInternalServerError, "persist env: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionEnvVarUpdate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "set env "+req.Key, "")
	w.WriteHeader(http.StatusNoContent)
}

// deleteApp removes a per-app env var by natural key.
func (h *envVarsHandler) deleteApp(w http.ResponseWriter, r *http.Request) {
	app, ok := h.lookupApp(w, r)
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	if !envVarKeyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "invalid key")
		return
	}
	if err := h.store.EnvVars.DeleteByAppAndKey(r.Context(), app.ID, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "env var not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "delete env: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionEnvVarDelete, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "deleted env "+key, "")
	w.WriteHeader(http.StatusNoContent)
}

// lookupApp resolves the {slug} URL parameter to an App. Writes the error
// response and returns ok=false when missing.
func (h *envVarsHandler) lookupApp(w http.ResponseWriter, r *http.Request) (domain.App, bool) {
	slug := chi.URLParam(r, "slug")
	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return domain.App{}, false
	}
	return app, true
}

// serialize maps store rows to the wire shape. When reveal is true, opener
// is invoked per row to decrypt the value.
func (h *envVarsHandler) serialize(rows []domain.EnvVar, reveal bool, opener envOpener) ([]envVarResponse, error) {
	out := make([]envVarResponse, 0, len(rows))
	for _, v := range rows {
		item := envVarResponse{
			Key:       v.Key,
			Masked:    !reveal,
			HasValue:  len(v.ValueEncrypted) > 0,
			UpdatedAt: v.UpdatedAt,
		}
		if reveal && len(v.ValueEncrypted) > 0 {
			plain, err := opener(v.Key, v.ValueEncrypted)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", v.Key, err)
			}
			item.Value = string(plain)
		}
		out = append(out, item)
	}
	return out, nil
}

// envOpener is "given the key and ciphertext, return the plaintext". Used
// to keep app- and shared-scoped reveal code symmetric.
type envOpener func(key string, ciphertext []byte) ([]byte, error)

func envvarAppOpener(appID int64, c *crypto.Codec) envOpener {
	return func(key string, ct []byte) ([]byte, error) {
		return c.Open(domain.CodecPurposeEnvVarApp, domain.EnvVarAppAAD(appID, key), ct)
	}
}

func envvarSharedOpener(c *crypto.Codec) envOpener {
	return func(key string, ct []byte) ([]byte, error) {
		return c.Open(domain.CodecPurposeEnvVarShared, domain.EnvVarSharedAAD(key), ct)
	}
}
