package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/github"
	"github.com/sariakos/teal/backend/internal/store"
)

// webhookHandler serves POST /api/v1/webhooks/github/{slug}. Unauthenticated
// by design: the request is authenticated via GitHub's HMAC signature,
// which requires the per-app webhook secret.
//
// The handler deliberately returns 401 for BOTH "unknown slug" and
// "invalid signature" so an attacker probing slugs can't distinguish the
// two cases.
type webhookHandler struct {
	logger *slog.Logger
	store  *store.Store
	codec  *crypto.Codec
	engine *deploy.Engine
}

// maxWebhookBody caps the body we accept. GitHub push payloads are
// typically < 30 kB; 1 MiB gives headroom for monorepo pushes with long
// commit lists without letting attackers exhaust memory.
const maxWebhookBody = 1 << 20

func (h *webhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	if len(body) > maxWebhookBody {
		writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}

	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		// Treat unknown slug identically to bad signature — don't leak
		// whether a slug exists on this instance.
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}
	if len(app.WebhookSecretEncrypted) == 0 {
		// App exists but hasn't generated a webhook secret yet. Same
		// response as bad slug so presence/absence is indistinguishable.
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}
	secret, err := h.codec.Open("webhook.secret", "app:"+strconv.FormatInt(app.ID, 10), app.WebhookSecretEncrypted)
	if err != nil {
		h.logger.Error("decrypt webhook secret", "app", slug, "err", err)
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}

	if !github.VerifySignature(secret, body, r.Header.Get(github.SignatureHeader)) {
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}

	// Valid signature from here on. Now branch on event type.
	evt := r.Header.Get(github.EventHeader)
	if evt == "ping" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}
	if evt != "push" {
		// We acknowledge non-push events so GitHub's delivery logs don't
		// fill with retries; we just don't act on them.
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "event is not push"})
		return
	}

	push, err := github.ParsePush(body)
	if err != nil {
		if errors.Is(err, github.ErrNotAPush) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "not a push payload"})
			return
		}
		writeError(w, http.StatusBadRequest, "parse push: "+err.Error())
		return
	}
	if push.Deleted {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "branch deleted"})
		return
	}
	branch := push.Branch()
	if branch == "" || branch != app.EffectiveGitBranch() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "branch does not match"})
		return
	}
	if !app.AutoDeployEnabled {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "auto-deploy disabled"})
		return
	}

	dep, err := h.engine.Trigger(r.Context(), app, nil, push.HeadCommit.ID, domain.TriggerWebhook)
	if err != nil {
		if errors.Is(err, deploy.ErrLocked) {
			// Audit the skipped delivery so operators can see why.
			recordAudit(r.Context(), h.logger, h.store.AuditLogs,
				domain.AuditActionDeploymentStart, "app", strconv.FormatInt(app.ID, 10),
				clientIP(r), "webhook deploy skipped (locked)", "github:webhook")
			writeError(w, http.StatusConflict, "another deployment is in progress")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to trigger deploy: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionDeploymentStart, "deployment", strconv.FormatInt(dep.ID, 10),
		clientIP(r), "webhook deploy from "+push.Repository.FullName+"@"+push.HeadCommit.ID, "github:webhook")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":       "accepted",
		"deploymentId": dep.ID,
		"commitSha":    push.HeadCommit.ID,
	})
}
