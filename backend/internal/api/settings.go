package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
	"github.com/sariakos/teal/backend/internal/traefik"
)

// settingResponse is the wire shape of a platform setting.
type settingResponse struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// settingMutationResponse is returned by upsert/delete. RestartTraefik is
// true when the change altered the static config, which Traefik only reads
// at boot — operator action is required to make it take effect.
type settingMutationResponse struct {
	RestartTraefik bool `json:"restartTraefik"`
}

// settingsHandler exposes admin-only platform settings (KV).
type settingsHandler struct {
	logger            *slog.Logger
	store             *store.Store
	traefikStaticPath string
	dashboardInsecure bool
}

// list returns every setting, ordered by key. We do NOT inject defaults
// here — clients render "(unset)" when a key is missing. Keeping the API
// faithful to the persisted state means nothing surprises an admin who
// later removes a key.
func (h *settingsHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.PlatformSettings.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list settings: "+err.Error())
		return
	}
	out := make([]settingResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, settingResponse{Key: s.Key, Value: s.Value, UpdatedAt: s.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

type upsertSettingRequest struct {
	Value string `json:"value"`
}

// upsert sets a single setting by key (in the URL). Whitelist of allowed
// keys is enforced here so a typo can't write a stray row that silently
// does nothing.
func (h *settingsHandler) upsert(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if !isAllowedSettingKey(key) {
		writeError(w, http.StatusBadRequest, "unknown setting key")
		return
	}
	var req upsertSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.store.PlatformSettings.Set(r.Context(), key, req.Value); err != nil {
		writeError(w, http.StatusInternalServerError, "persist setting: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionPlatformSettingSet, "platform_setting", key,
		clientIP(r), "set "+key, "")
	restart := h.regenerateStatic(r, key)
	writeJSON(w, http.StatusOK, settingMutationResponse{RestartTraefik: restart})
}

// delete clears a setting. Idempotent: deleting an unset key returns 204.
func (h *settingsHandler) delete(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if !isAllowedSettingKey(key) {
		writeError(w, http.StatusBadRequest, "unknown setting key")
		return
	}
	if err := h.store.PlatformSettings.Delete(r.Context(), key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, settingMutationResponse{RestartTraefik: false})
			return
		}
		writeError(w, http.StatusInternalServerError, "delete setting: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionPlatformSettingSet, "platform_setting", key,
		clientIP(r), "cleared "+key, "")
	restart := h.regenerateStatic(r, key)
	writeJSON(w, http.StatusOK, settingMutationResponse{RestartTraefik: restart})
}

// regenerateStatic rewrites the Traefik static config when the changed
// key affects it (anything in acme.* today). Returns true when the file
// was rewritten, signalling that the operator must restart Traefik.
//
// When TraefikStaticPath is empty (e.g. integration tests) we skip the
// write but return true for acme keys so callers can still see the
// "you should restart" hint in their tests.
func (h *settingsHandler) regenerateStatic(r *http.Request, changedKey string) bool {
	if !affectsStatic(changedKey) {
		return false
	}
	if h.traefikStaticPath == "" {
		return true
	}
	if err := traefik.ApplyStaticFromSettings(r.Context(), h.store.PlatformSettings, h.traefikStaticPath, h.dashboardInsecure); err != nil {
		h.logger.Error("regenerate traefik static config", "err", err)
	}
	return true
}

func affectsStatic(key string) bool {
	switch key {
	case domain.SettingACMEEmail, domain.SettingACMEStaging:
		return true
	}
	return false
}

// isAllowedSettingKey enforces the whitelist of platform setting keys
// the API understands. Adding a new tunable means landing it here.
func isAllowedSettingKey(key string) bool {
	switch key {
	case domain.SettingACMEEmail,
		domain.SettingACMEStaging,
		domain.SettingHTTPSRedirect,
		"smtp.host",
		"smtp.port",
		"smtp.user",
		"smtp.pass",
		"smtp.from",
		"smtp.starttls",
		// GitHub App identity. The two secret keys (private_key_b64 and
		// webhook_secret_b64) are written via a dedicated handler that
		// encrypts before persisting — they can't be set through the
		// generic /settings/{key} surface.
		"github_app.app_id",
		"github_app.app_slug":
		return true
	}
	return false
}
