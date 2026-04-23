package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/git"
	"github.com/sariakos/teal/backend/internal/store"
)

// deployKeyResponse is the wire shape of GET /apps/{slug}/deploy-key.
type deployKeyResponse struct {
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
}

// deployKey returns the App's current SSH deploy-key public half. Only
// available when gitAuthKind == "ssh" AND a credential is stored.
//
// We rederive the public from the stored private rather than keeping a
// redundant column — avoids drift between the two halves after rotation.
func (h *appsHandler) deployKey(w http.ResponseWriter, r *http.Request) {
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
	if app.GitAuthKind != domain.GitAuthSSH || len(app.GitAuthCredentialEncrypted) == 0 {
		writeError(w, http.StatusNotFound, "no ssh deploy key configured for this app")
		return
	}
	pem, err := h.codec.Open("git.private_key", "app:"+strconv.FormatInt(app.ID, 10), app.GitAuthCredentialEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "decrypt key")
		return
	}
	pub, err := git.PublicKeyFromPrivatePEM(pem, app.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "derive public key")
		return
	}
	fp, _ := git.SSHKeyFingerprint(pub)
	writeJSON(w, http.StatusOK, deployKeyResponse{PublicKey: pub, Fingerprint: fp})
}

// rotateDeployKey issues a fresh SSH keypair for the app. Returns the NEW
// public half in the response (once). The old private is overwritten —
// existing uses of the old key will stop working once the user removes it
// from GitHub.
func (h *appsHandler) rotateDeployKey(w http.ResponseWriter, r *http.Request) {
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

	privPEM, publicSSH, err := git.GenerateSSHKeyPair(app.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate key")
		return
	}
	enc, err := h.encryptCredential(app.ID, privPEM)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encrypt key")
		return
	}
	app.GitAuthKind = domain.GitAuthSSH
	app.GitAuthCredentialEncrypted = enc
	if err := h.store.Apps.Update(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "persist key")
		return
	}
	fp, _ := git.SSHKeyFingerprint(publicSSH)
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppUpdate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "rotated deploy key", "")
	writeJSON(w, http.StatusOK, deployKeyResponse{PublicKey: publicSSH, Fingerprint: fp})
}

// rotateWebhookSecretResponse is the one-shot response for rotation.
type rotateWebhookSecretResponse struct {
	WebhookSecret string `json:"webhookSecret"`
}

// rotateWebhookSecret issues a fresh HMAC secret. Returns the raw value
// once. Callers must update GitHub with the new secret.
func (h *appsHandler) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
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
	raw, enc, err := h.newWebhookSecret(app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate secret")
		return
	}
	app.WebhookSecretEncrypted = enc
	if err := h.store.Apps.Update(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "persist secret")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppUpdate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "rotated webhook secret", "")
	writeJSON(w, http.StatusOK, rotateWebhookSecretResponse{WebhookSecret: raw})
}
