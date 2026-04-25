package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/store"
)

// requireSubject pulls the authenticated principal off ctx; 401s when
// missing. Mirrors the inline pattern in github_app.go's startInstall
// so handlers don't reach into auth.FromContext directly.
func requireSubject(w http.ResponseWriter, r *http.Request) (auth.Subject, bool) {
	s := auth.FromContext(r.Context())
	if s.IsZero() {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return s, false
	}
	return s, true
}

// gitHubAppManifestHandler implements the one-click "create the
// platform GitHub App" flow. Two endpoints:
//
//   POST /api/v1/settings/github-app/manifest-init  (admin)
//        Returns the manifest JSON + the GitHub URL the browser must
//        POST it to. Frontend builds an auto-submitted form.
//
//   GET /api/v1/settings/github-app/manifest-callback  (unauth — GitHub
//        is the caller). Exchanges the temporary code for App
//        credentials, persists them via the existing settings layer,
//        redirects the browser to the admin UI.
//
// State (HMAC-signed nonce) round-trips through GitHub so the
// callback can't be forged from a stale URL someone fishes out of
// browser history. We piggy-back on the per-app install state's
// signer + parser; Slug is repurposed as a flow marker.
type gitHubAppManifestHandler struct {
	logger        *slog.Logger
	store         *store.Store
	codec         *crypto.Codec
	stateSecret   []byte
	publicBaseURL string
	httpDoer      githubapp.HTTPDoer // nil → http.DefaultClient
}

const manifestStateMarker = "__teal_manifest__"

type manifestInitRequest struct {
	// Org, when non-empty, creates an organization-owned App. Empty
	// means user-owned (the operator becomes the owner). The operator
	// must be an org admin or have the right permission to install.
	Org string `json:"org"`
}

type manifestInitResponse struct {
	// Manifest is the JSON document the browser must POST to
	// PostURL. Frontend stringifies + drops it into a hidden form
	// field named "manifest".
	Manifest githubapp.Manifest `json:"manifest"`

	// PostURL is the github.com URL the form must POST to. Differs
	// for user vs org ownership.
	PostURL string `json:"postUrl"`

	// State is the HMAC-signed nonce GitHub round-trips to the
	// callback. Must be sent as the "state" form field alongside
	// "manifest".
	State string `json:"state"`
}

func (h *gitHubAppManifestHandler) init(w http.ResponseWriter, r *http.Request) {
	if h.publicBaseURL == "" {
		writeError(w, http.StatusBadRequest, "TEAL_BASE_DOMAIN is not configured — set it before creating an App so GitHub knows where to redirect")
		return
	}
	var req manifestInitRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // empty body is OK

	subj, ok := requireSubject(w, r)
	if !ok {
		return
	}

	state, err := githubapp.SignState(h.stateSecret, manifestStateMarker, subj.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sign state: "+err.Error())
		return
	}

	manifest := githubapp.BuildManifest(h.publicBaseURL, manifestName(h.publicBaseURL))

	writeJSON(w, http.StatusOK, manifestInitResponse{
		Manifest: manifest,
		PostURL:  githubapp.ManifestCreateURL(strings.TrimSpace(req.Org)),
		State:    state,
	})
}

// callback handles GitHub's redirect after the operator clicks
// "Create" on github.com. We don't gate it with auth — GitHub is the
// caller, the state HMAC is the bearer of authority.
func (h *gitHubAppManifestHandler) callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		h.fail(w, "missing code or state in callback")
		return
	}
	claims, err := githubapp.ParseState(h.stateSecret, state)
	if err != nil {
		h.fail(w, "invalid state: "+err.Error())
		return
	}
	if claims.Slug != manifestStateMarker {
		h.fail(w, "state was issued for a different flow")
		return
	}

	conv, err := githubapp.ExchangeManifestCode(r.Context(), h.httpDoer, code)
	if err != nil {
		h.fail(w, "exchange: "+err.Error())
		return
	}

	// Persist the App identity + secrets via the same primitives the
	// manual paste form uses, so a future paste can rotate either.
	if err := h.store.PlatformSettings.Set(r.Context(),
		githubapp.SettingAppID, strconv.FormatInt(conv.ID, 10)); err != nil {
		h.fail(w, "persist app id: "+err.Error())
		return
	}
	if err := h.store.PlatformSettings.Set(r.Context(),
		githubapp.SettingAppSlug, conv.Slug); err != nil {
		h.fail(w, "persist app slug: "+err.Error())
		return
	}
	if conv.PEM != "" {
		if err := githubapp.SaveSecret(r.Context(), h.store, h.codec,
			githubapp.CodecPurposePrivateKey, githubapp.SettingPrivateKeyEncryptedB64,
			[]byte(conv.PEM)); err != nil {
			h.fail(w, "encrypt private key: "+err.Error())
			return
		}
	}
	if conv.WebhookSecret != "" {
		if err := githubapp.SaveSecret(r.Context(), h.store, h.codec,
			githubapp.CodecPurposeWebhookSecret, githubapp.SettingWebhookSecretEncryptedB64,
			[]byte(conv.WebhookSecret)); err != nil {
			h.fail(w, "encrypt webhook secret: "+err.Error())
			return
		}
	}

	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionPlatformSettingSet, "github_app", "config",
		clientIP(r), "created via GitHub App manifest flow", conv.Slug)

	// Redirect the browser back to the admin UI with a flag the page
	// reads to show "App created — install it on a repo next" banner.
	target := "/settings/github-app?created=" + conv.Slug
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// fail is the user-facing error path. GitHub redirected the browser
// here, so we owe the operator an HTML page rather than a JSON
// blob — the admin UI doesn't see this response.
func (h *gitHubAppManifestHandler) fail(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(`<!doctype html><meta charset=utf-8><title>Teal — manifest flow error</title>` +
		`<style>body{font-family:system-ui;max-width:560px;margin:6em auto;padding:0 1em;color:#333}` +
		`code{background:#f4f4f4;padding:2px 6px;border-radius:4px}</style>` +
		`<h1>Manifest flow failed</h1>` +
		`<p>The error was: <code>` + htmlEscape(msg) + `</code></p>` +
		`<p>Try again from <a href="/settings/github-app">Settings → GitHub App</a>, ` +
		`or fall back to the manual create flow on the same page.</p>`))
}

// htmlEscape is a minimal escape for the four characters that bite in
// HTML text content. Avoids pulling html/template just for this.
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// manifestName picks a unique-enough App name from the base URL.
// GitHub requires globally-unique names for user-owned apps; using
// the operator's hostname makes collisions essentially impossible
// across separate Teal installs.
func manifestName(baseURL string) string {
	host := baseURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		host = "platform"
	}
	return "Teal — " + host
}
