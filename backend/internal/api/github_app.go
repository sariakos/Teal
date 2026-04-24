package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/store"
)

// gitHubAppHandler exposes the per-app install flow:
//
//   POST /apps/{slug}/install-github-app   → returns the github.com URL
//                                            the operator should visit
//   GET  /github-app/setup-callback        → GitHub's redirect lands
//                                            here with installation_id
//                                            + state; we verify the
//                                            state, store the linkage,
//                                            redirect back to the app
//                                            detail page.
//
// The platform-wide App credentials handler lives in github_app_admin.go
// (commit 4); this file is per-app linkage only.
type gitHubAppHandler struct {
	logger        *slog.Logger
	store         *store.Store
	codec         *crypto.Codec
	tokenCache    *githubapp.TokenCache
	stateSecret   []byte // platform secret; used for HMAC on install state
	publicBaseURL string // e.g. https://srv.sariakos.com — used to build callback URL hint
}

// installResponse is what POST /apps/{slug}/install-github-app returns.
// The UI redirects the user's browser to InstallURL.
type installResponse struct {
	InstallURL  string `json:"installUrl"`
	CallbackURL string `json:"callbackUrl"`
}

func (h *gitHubAppHandler) startInstall(w http.ResponseWriter, r *http.Request) {
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
	cfg, err := githubapp.LoadConfig(r.Context(), h.store, h.codec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load github app config: "+err.Error())
		return
	}
	if !cfg.Configured() || cfg.AppSlug == "" {
		writeError(w, http.StatusBadRequest,
			"GitHub App not configured (admin: open /settings/github-app and set the App ID, slug, private key, and webhook secret first)")
		return
	}

	subj := auth.FromContext(r.Context())
	state, err := githubapp.SignState(h.stateSecret, app.Slug, subj.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sign state: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, installResponse{
		InstallURL:  githubapp.InstallURL(cfg.AppSlug, state),
		CallbackURL: h.publicBaseURL + "/api/v1/github-app/setup-callback",
	})
}

// setupCallback is the redirect target GitHub sends users to after they
// pick which repos to install on. Query params (per GitHub docs):
//
//   installation_id  numeric, the new installation's ID
//   setup_action     "install" or "update"
//   state            the value we passed in the install URL
//
// We verify the state, persist installation_id on the app, then redirect
// the browser back to the app detail page.
func (h *gitHubAppHandler) setupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	installIDStr := q.Get("installation_id")
	if state == "" || installIDStr == "" {
		http.Error(w, "missing state or installation_id", http.StatusBadRequest)
		return
	}
	claims, err := githubapp.ParseState(h.stateSecret, state)
	if err != nil {
		http.Error(w, "invalid state: "+err.Error(), http.StatusBadRequest)
		return
	}
	installationID, err := strconv.ParseInt(installIDStr, 10, 64)
	if err != nil || installationID <= 0 {
		http.Error(w, "installation_id must be a positive integer", http.StatusBadRequest)
		return
	}

	app, err := h.store.Apps.GetBySlug(r.Context(), claims.Slug)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	app.GitHubAppInstallationID = installationID
	app.GitAuthKind = domain.GitAuthGitHubApp

	// Best-effort: ask GitHub which repo this install covers. If the
	// install is "All repositories" or the API call hiccups, we leave
	// GitHubAppRepo empty — the user can fill it in manually from the
	// Settings tab. Don't block the callback on this.
	cfg, err := githubapp.LoadConfig(r.Context(), h.store, h.codec)
	if err == nil && cfg.Configured() {
		if h.tokenCache != nil {
			if tok, err := h.tokenCache.Get(r.Context(), cfg, installationID); err == nil {
				if repo, err := fetchInstallationRepoFromAPI(r.Context(), tok.Token); err == nil {
					app.GitHubAppRepo = repo
				}
			}
		}
	}

	if err := h.store.Apps.Update(r.Context(), app); err != nil {
		http.Error(w, "persist install: "+err.Error(), http.StatusInternalServerError)
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppUpdate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "linked GitHub App installation "+installIDStr, "")

	// Redirect back to the app detail page. Use a relative URL so it
	// works whether or not publicBaseURL was set.
	http.Redirect(w, r, "/apps/"+app.Slug+"?installed=github_app", http.StatusFound)
}
