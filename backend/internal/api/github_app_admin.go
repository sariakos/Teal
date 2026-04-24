package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/store"
)

// gitHubAppAdminHandler exposes admin-only management of the platform-
// wide GitHub App credentials. Lives separately from
// gitHubAppHandler (per-app install flow) to keep the auth boundary
// obvious — these handlers are admin-gated in the router.
type gitHubAppAdminHandler struct {
	logger *slog.Logger
	store  *store.Store
	codec  *crypto.Codec
}

// gitHubAppConfigResponse is what GET /settings/github-app returns.
// HasPrivateKey/HasWebhookSecret tell the UI whether the secrets are
// already stored without exposing the values.
type gitHubAppConfigResponse struct {
	AppID            int64  `json:"appId"`
	AppSlug          string `json:"appSlug"`
	HasPrivateKey    bool   `json:"hasPrivateKey"`
	HasWebhookSecret bool   `json:"hasWebhookSecret"`
}

func (h *gitHubAppAdminHandler) get(w http.ResponseWriter, r *http.Request) {
	cfg, err := githubapp.LoadConfig(r.Context(), h.store, h.codec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gitHubAppConfigResponse{
		AppID:            cfg.AppID,
		AppSlug:          cfg.AppSlug,
		HasPrivateKey:    len(cfg.PrivateKeyPEM) > 0,
		HasWebhookSecret: len(cfg.WebhookSecret) > 0,
	})
}

type updateGitHubAppRequest struct {
	AppID         *int64  `json:"appId"`
	AppSlug       *string `json:"appSlug"`
	PrivateKeyPEM *string `json:"privateKeyPem"`
	WebhookSecret *string `json:"webhookSecret"`
}

// put accepts a partial update — any unset field stays as-is. Lets
// the admin rotate the webhook secret without re-uploading the
// private key, etc.
func (h *gitHubAppAdminHandler) put(w http.ResponseWriter, r *http.Request) {
	var req updateGitHubAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.AppID != nil {
		if *req.AppID < 0 {
			writeError(w, http.StatusBadRequest, "appId must be >= 0 (use 0 to clear)")
			return
		}
		if err := h.store.PlatformSettings.Set(r.Context(),
			githubapp.SettingAppID, strconv.FormatInt(*req.AppID, 10)); err != nil {
			writeError(w, http.StatusInternalServerError, "persist appId: "+err.Error())
			return
		}
	}
	if req.AppSlug != nil {
		slug := strings.TrimSpace(*req.AppSlug)
		if err := h.store.PlatformSettings.Set(r.Context(), githubapp.SettingAppSlug, slug); err != nil {
			writeError(w, http.StatusInternalServerError, "persist appSlug: "+err.Error())
			return
		}
	}
	if req.PrivateKeyPEM != nil && strings.TrimSpace(*req.PrivateKeyPEM) != "" {
		if err := githubapp.SaveSecret(r.Context(), h.store, h.codec,
			githubapp.CodecPurposePrivateKey, githubapp.SettingPrivateKeyEncryptedB64,
			[]byte(*req.PrivateKeyPEM)); err != nil {
			writeError(w, http.StatusInternalServerError, "encrypt private key: "+err.Error())
			return
		}
	}
	if req.WebhookSecret != nil && strings.TrimSpace(*req.WebhookSecret) != "" {
		if err := githubapp.SaveSecret(r.Context(), h.store, h.codec,
			githubapp.CodecPurposeWebhookSecret, githubapp.SettingWebhookSecretEncryptedB64,
			[]byte(*req.WebhookSecret)); err != nil {
			writeError(w, http.StatusInternalServerError, "encrypt webhook secret: "+err.Error())
			return
		}
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionPlatformSettingSet, "github_app", "config",
		clientIP(r), "updated GitHub App configuration", "")
	w.WriteHeader(http.StatusNoContent)
}
