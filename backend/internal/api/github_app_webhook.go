package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/github"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/store"
)

// gitHubAppWebhookHandler serves POST /api/v1/webhooks/github-app —
// the SINGLE webhook URL configured in the platform's GitHub App on
// github.com. Every push from every installation lands here; we route
// to the right Teal app by matching repository.full_name against
// app.github_app_repo.
//
// Authentication is HMAC-SHA256 against the platform-wide webhook
// secret stored in platform_settings (encrypted under
// CodecPurposeWebhookSecret). Per-app secrets aren't used at all on
// this path.
//
// Per-app /webhooks/github/{slug} continues to serve SSH/PAT-based
// apps; this endpoint coexists rather than replacing it.
type gitHubAppWebhookHandler struct {
	logger *slog.Logger
	store  *store.Store
	codec  *crypto.Codec
	engine *deploy.Engine
}

func (h *gitHubAppWebhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	if len(body) > maxWebhookBody {
		writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}

	cfg, err := githubapp.LoadConfig(r.Context(), h.store, h.codec)
	if err != nil {
		h.logger.Error("github app webhook: load config", "err", err)
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}
	// "Not configured" and "bad signature" return the same status so
	// scanners can't tell whether the App is set up.
	if !cfg.Configured() || len(cfg.WebhookSecret) == 0 {
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}
	if !github.VerifySignature(cfg.WebhookSecret, body, r.Header.Get(github.SignatureHeader)) {
		writeError(w, http.StatusUnauthorized, "webhook validation failed")
		return
	}

	evt := r.Header.Get(github.EventHeader)
	if evt == "ping" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}
	if evt != "push" {
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

	app, err := githubapp.FindAppByRepo(r.Context(), h.store, push.Repository.FullName)
	if err != nil {
		// No Teal app is installed for this repo. Returning 200 (ignored)
		// keeps GitHub's delivery log clean — they retry on 4xx/5xx.
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ignored",
			"reason": "no app installed for " + push.Repository.FullName,
		})
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
			recordAudit(r.Context(), h.logger, h.store.AuditLogs,
				domain.AuditActionDeploymentStart, "app", strconv.FormatInt(app.ID, 10),
				clientIP(r), "github-app webhook deploy skipped (locked)", "github_app:webhook")
			writeError(w, http.StatusConflict, "another deployment is in progress")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to trigger deploy: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionDeploymentStart, "deployment", strconv.FormatInt(dep.ID, 10),
		clientIP(r), "github-app webhook deploy from "+push.Repository.FullName+"@"+push.HeadCommit.ID, "github_app:webhook")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":       "accepted",
		"deploymentId": dep.ID,
		"commitSha":    push.HeadCommit.ID,
	})
}
