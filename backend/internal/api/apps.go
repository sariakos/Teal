package api

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/git"
	"github.com/sariakos/teal/backend/internal/store"
)

// appResponse is the wire shape of an App. Defined separately from
// domain.App so adding fields here doesn't silently leak schema-internal
// state to clients.
type appResponse struct {
	ID                    int64     `json:"id"`
	Slug                  string    `json:"slug"`
	Name                  string    `json:"name"`
	Domains               []string  `json:"domains"`
	ActiveColor           string    `json:"activeColor,omitempty"`
	AutoDeployBranch      string    `json:"autoDeployBranch"`
	AutoDeployEnabled     bool      `json:"autoDeployEnabled"`
	Status                string    `json:"status"`
	LastDeployedCommitSHA string    `json:"lastDeployedCommitSha,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

// appDetailResponse extends appResponse with the compose file plus the git
// configuration view (never including the encrypted credential itself).
type appDetailResponse struct {
	appResponse
	ComposeFile               string `json:"composeFile"`
	GitURL                    string `json:"gitUrl,omitempty"`
	GitAuthKind               string `json:"gitAuthKind,omitempty"`
	GitBranch                 string `json:"gitBranch,omitempty"`
	GitComposePath            string `json:"gitComposePath,omitempty"`
	HasGitCredential          bool   `json:"hasGitCredential"`
	HasWebhookSecret          bool   `json:"hasWebhookSecret"`
	CPULimit                  string `json:"cpuLimit,omitempty"`
	MemoryLimit               string `json:"memoryLimit,omitempty"`
	NotificationWebhookURL    string `json:"notificationWebhookUrl,omitempty"`
	HasNotificationSecret     bool   `json:"hasNotificationSecret"`
	NotificationEmail         string `json:"notificationEmail,omitempty"`

	// GitHub App linkage. InstallationID == 0 means not yet installed.
	GitHubAppInstallationID int64  `json:"githubAppInstallationId,omitempty"`
	GitHubAppRepo           string `json:"githubAppRepo,omitempty"`
}

// appInitSecretsResponse extends appDetailResponse with ONE-SHOT secrets
// returned only when the fields were freshly generated on this request:
// the raw webhook secret (every time webhook config is saved fresh/rotated)
// and, for SSH, the public key (the private half never leaves the server).
type appInitSecretsResponse struct {
	appDetailResponse
	NewWebhookSecret string `json:"newWebhookSecret,omitempty"`
	NewPublicKey     string `json:"newPublicKey,omitempty"`
	NewKeyFingerprint string `json:"newKeyFingerprint,omitempty"`
}

func appToResponse(a domain.App) appResponse {
	return appResponse{
		ID:                    a.ID,
		Slug:                  a.Slug,
		Name:                  a.Name,
		Domains:               splitDomainsField(a.Domains),
		ActiveColor:           string(a.ActiveColor),
		AutoDeployBranch:      a.AutoDeployBranch,
		AutoDeployEnabled:     a.AutoDeployEnabled,
		Status:                string(a.Status),
		LastDeployedCommitSHA: a.LastDeployedCommitSHA,
		CreatedAt:             a.CreatedAt,
		UpdatedAt:             a.UpdatedAt,
	}
}

func appToDetail(a domain.App) appDetailResponse {
	return appDetailResponse{
		appResponse:               appToResponse(a),
		ComposeFile:               a.ComposeFile,
		GitURL:                    a.GitURL,
		GitAuthKind:               string(a.GitAuthKind),
		GitBranch:                 a.GitBranch,
		GitComposePath:            a.GitComposePath,
		HasGitCredential:          len(a.GitAuthCredentialEncrypted) > 0,
		HasWebhookSecret:          len(a.WebhookSecretEncrypted) > 0,
		CPULimit:                  a.CPULimit,
		MemoryLimit:               a.MemoryLimit,
		NotificationWebhookURL:    a.NotificationWebhookURL,
		HasNotificationSecret:     len(a.NotificationWebhookSecretEncrypted) > 0,
		NotificationEmail:         a.NotificationEmail,
		GitHubAppInstallationID:   a.GitHubAppInstallationID,
		GitHubAppRepo:             a.GitHubAppRepo,
	}
}

// appsHandler holds dependencies for App-related endpoints.
type appsHandler struct {
	logger *slog.Logger
	store  *store.Store
	engine *deploy.Engine
	codec  *crypto.Codec
}

// list returns every App.
func (h *appsHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.Apps.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list apps")
		return
	}
	out := make([]appResponse, 0, len(rows))
	for _, a := range rows {
		out = append(out, appToResponse(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *appsHandler) get(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, appToDetail(app))
}

type createAppRequest struct {
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	ComposeFile       string   `json:"composeFile"`
	Domains           []string `json:"domains"`
	AutoDeployBranch  string   `json:"autoDeployBranch"`
	AutoDeployEnabled bool     `json:"autoDeployEnabled"`

	// Git source fields — mirror the PATCH endpoint so an app can be
	// created git-ready in one round-trip. When GitURL is set, the
	// response includes one-shot secrets (newPublicKey /
	// newWebhookSecret) the UI must surface immediately.
	GitURL         string `json:"gitUrl"`
	GitAuthKind    string `json:"gitAuthKind"`     // "" | "ssh" | "pat"
	GitCredential  string `json:"gitCredential"`   // PEM (SSH) or raw PAT
	GitBranch      string `json:"gitBranch"`
	GitComposePath string `json:"gitComposePath"`
}

func (h *appsHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Slug = strings.TrimSpace(strings.ToLower(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !validSlug(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must match [a-z][a-z0-9-]*[a-z0-9] (3..40 chars)")
		return
	}
	gitKind := domain.GitAuthKind(strings.TrimSpace(req.GitAuthKind))
	if err := validateGitAuthKind(gitKind); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	gitURL := strings.TrimSpace(req.GitURL)
	if gitURL != "" && gitKind == domain.GitAuthPAT && req.GitCredential == "" {
		writeError(w, http.StatusBadRequest, "PAT auth requires gitCredential on first set")
		return
	}
	gitComposePath := strings.TrimSpace(req.GitComposePath)
	if gitComposePath == "" {
		gitComposePath = "docker-compose.yml"
	}

	app, err := h.store.Apps.Create(r.Context(), domain.App{
		Slug: req.Slug, Name: req.Name, ComposeFile: req.ComposeFile,
		Domains: joinDomains(req.Domains), AutoDeployBranch: req.AutoDeployBranch,
		AutoDeployEnabled: req.AutoDeployEnabled,
		GitURL:         gitURL,
		GitAuthKind:    gitKind,
		GitBranch:      strings.TrimSpace(req.GitBranch),
		GitComposePath: gitComposePath,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "slug already in use")
			return
		}
		// Log the underlying error server-side; surface a concise
		// message + the error class to the client so a 500 here is
		// debuggable without digging through container logs.
		h.logger.Error("apps.Create failed", "slug", req.Slug, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create app: "+err.Error())
		return
	}

	// Git side-effects (deploy keypair, webhook secret) need the app's
	// ID, so they happen post-Create. The response surfaces one-shot
	// reveals so the UI can show them before navigating away.
	init := appInitSecretsResponse{}
	if gitURL != "" {
		switch gitKind {
		case domain.GitAuthSSH:
			if req.GitCredential != "" {
				enc, err := h.encryptCredential(app.ID, []byte(req.GitCredential))
				if err != nil {
					writeError(w, http.StatusInternalServerError, "encrypt credential")
					return
				}
				app.GitAuthCredentialEncrypted = enc
			} else {
				privPEM, publicSSH, err := git.GenerateSSHKeyPair(app.Slug)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "generate deploy key")
					return
				}
				enc, err := h.encryptCredential(app.ID, privPEM)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "encrypt key")
					return
				}
				app.GitAuthCredentialEncrypted = enc
				init.NewPublicKey = publicSSH
				if fp, err := git.SSHKeyFingerprint(publicSSH); err == nil {
					init.NewKeyFingerprint = fp
				}
			}
		case domain.GitAuthPAT:
			enc, err := h.encryptCredential(app.ID, []byte(req.GitCredential))
			if err != nil {
				writeError(w, http.StatusInternalServerError, "encrypt credential")
				return
			}
			app.GitAuthCredentialEncrypted = enc
		}

		raw, encSec, err := h.newWebhookSecret(app.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate webhook secret")
			return
		}
		app.WebhookSecretEncrypted = encSec
		init.NewWebhookSecret = raw

		if err := h.store.Apps.Update(r.Context(), app); err != nil {
			writeError(w, http.StatusInternalServerError, "persist git config: "+err.Error())
			return
		}
	}

	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppCreate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "created app "+app.Slug, "")

	init.appDetailResponse = appToDetail(app)
	writeJSON(w, http.StatusCreated, init)
}

type updateAppRequest struct {
	Name              *string   `json:"name"`
	ComposeFile       *string   `json:"composeFile"`
	Domains           *[]string `json:"domains"`
	AutoDeployBranch  *string   `json:"autoDeployBranch"`
	AutoDeployEnabled *bool     `json:"autoDeployEnabled"`
	GitURL            *string   `json:"gitUrl"`
	GitAuthKind       *string   `json:"gitAuthKind"`     // "" | "ssh" | "pat"
	GitCredential     *string   `json:"gitCredential"`   // PEM or raw PAT; ignored for "ssh" + empty
	GitBranch         *string   `json:"gitBranch"`
	GitComposePath    *string   `json:"gitComposePath"`

	CPULimit               *string `json:"cpuLimit"`
	MemoryLimit            *string `json:"memoryLimit"`
	NotificationWebhookURL *string `json:"notificationWebhookUrl"`
	NotificationEmail      *string `json:"notificationEmail"`

	// GitHub App: linking to an installation lives in the install flow
	// (POST /apps/{slug}/install-github-app); these fields exist so an
	// admin can clear or fix-up the linkage manually if needed.
	GitHubAppInstallationID *int64  `json:"githubAppInstallationId"`
	GitHubAppRepo           *string `json:"githubAppRepo"`
}

func (h *appsHandler) update(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var req updateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if req.Name != nil {
		app.Name = strings.TrimSpace(*req.Name)
	}
	if req.ComposeFile != nil {
		app.ComposeFile = *req.ComposeFile
	}
	if req.Domains != nil {
		app.Domains = joinDomains(*req.Domains)
	}
	if req.AutoDeployBranch != nil {
		app.AutoDeployBranch = *req.AutoDeployBranch
	}
	if req.AutoDeployEnabled != nil {
		app.AutoDeployEnabled = *req.AutoDeployEnabled
	}
	if req.GitURL != nil {
		app.GitURL = strings.TrimSpace(*req.GitURL)
	}
	if req.GitBranch != nil {
		app.GitBranch = strings.TrimSpace(*req.GitBranch)
	}
	if req.GitComposePath != nil {
		p := strings.TrimSpace(*req.GitComposePath)
		if p == "" {
			p = "docker-compose.yml"
		}
		app.GitComposePath = p
	}
	if req.CPULimit != nil {
		v := strings.TrimSpace(*req.CPULimit)
		if v != "" && !validCPULimit(v) {
			writeError(w, http.StatusBadRequest, `cpuLimit must be a number, e.g. "0.5" or "2"`)
			return
		}
		app.CPULimit = v
	}
	if req.MemoryLimit != nil {
		v := strings.TrimSpace(*req.MemoryLimit)
		if v != "" && !validMemoryLimit(v) {
			writeError(w, http.StatusBadRequest, `memoryLimit must match docker grammar, e.g. "256m" or "1g"`)
			return
		}
		app.MemoryLimit = v
	}
	if req.NotificationWebhookURL != nil {
		u := strings.TrimSpace(*req.NotificationWebhookURL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			writeError(w, http.StatusBadRequest, "notificationWebhookUrl must be http:// or https://")
			return
		}
		app.NotificationWebhookURL = u
	}
	if req.NotificationEmail != nil {
		app.NotificationEmail = strings.TrimSpace(*req.NotificationEmail)
	}
	if req.GitHubAppInstallationID != nil {
		app.GitHubAppInstallationID = *req.GitHubAppInstallationID
	}
	if req.GitHubAppRepo != nil {
		app.GitHubAppRepo = strings.TrimSpace(*req.GitHubAppRepo)
	}

	// Git-auth handling. The tricky case is SSH with no user-provided
	// credential: we generate a keypair and return the public half once.
	init := appInitSecretsResponse{}
	if req.GitAuthKind != nil {
		newKind := domain.GitAuthKind(*req.GitAuthKind)
		if err := validateGitAuthKind(newKind); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		app.GitAuthKind = newKind
		switch newKind {
		case domain.GitAuthNone:
			app.GitAuthCredentialEncrypted = nil
		case domain.GitAuthSSH:
			if req.GitCredential != nil && *req.GitCredential != "" {
				enc, err := h.encryptCredential(app.ID, []byte(*req.GitCredential))
				if err != nil {
					writeError(w, http.StatusInternalServerError, "encrypt credential")
					return
				}
				app.GitAuthCredentialEncrypted = enc
			} else if len(app.GitAuthCredentialEncrypted) == 0 {
				privPEM, publicSSH, err := git.GenerateSSHKeyPair(app.Slug)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "generate deploy key")
					return
				}
				enc, err := h.encryptCredential(app.ID, privPEM)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "encrypt key")
					return
				}
				app.GitAuthCredentialEncrypted = enc
				init.NewPublicKey = publicSSH
				if fp, err := git.SSHKeyFingerprint(publicSSH); err == nil {
					init.NewKeyFingerprint = fp
				}
			}
		case domain.GitAuthPAT:
			if req.GitCredential == nil || *req.GitCredential == "" {
				if len(app.GitAuthCredentialEncrypted) == 0 {
					writeError(w, http.StatusBadRequest, "PAT auth requires gitCredential on first set")
					return
				}
			} else {
				enc, err := h.encryptCredential(app.ID, []byte(*req.GitCredential))
				if err != nil {
					writeError(w, http.StatusInternalServerError, "encrypt credential")
					return
				}
				app.GitAuthCredentialEncrypted = enc
			}
		}
	}

	// Generate a webhook secret the first time a git source is configured.
	if app.GitURL != "" && len(app.WebhookSecretEncrypted) == 0 {
		raw, enc, err := h.newWebhookSecret(app.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate webhook secret")
			return
		}
		app.WebhookSecretEncrypted = enc
		init.NewWebhookSecret = raw
	}

	// Generate the outbound notification webhook secret the first time
	// a notification URL is configured. Returned once via the same
	// init.NewWebhookSecret one-shot field — the UI surfaces the
	// secret in a copy-now banner regardless of which trigger created it.
	if app.NotificationWebhookURL != "" && len(app.NotificationWebhookSecretEncrypted) == 0 {
		raw, _, err := h.newWebhookSecret(app.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate notification secret")
			return
		}
		enc, err := h.codec.Seal("webhook.outbound", "app:"+strconv.FormatInt(app.ID, 10)+":notify", []byte(raw))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "seal notification secret")
			return
		}
		app.NotificationWebhookSecretEncrypted = enc
		// Don't overwrite a webhook (inbound) secret from the same
		// PATCH; if both fired, prefer the inbound one (it's older
		// behaviour from Phase 4).
		if init.NewWebhookSecret == "" {
			init.NewWebhookSecret = raw
		}
	}

	if err := h.store.Apps.Update(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update app")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppUpdate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "updated app "+app.Slug, "")

	init.appDetailResponse = appToDetail(app)
	writeJSON(w, http.StatusOK, init)
}

// encryptCredential wraps Codec.Seal with the git credential purpose and
// the app-bound AAD we standardise on.
func (h *appsHandler) encryptCredential(appID int64, plaintext []byte) ([]byte, error) {
	return h.codec.Seal("git.private_key", "app:"+strconv.FormatInt(appID, 10), plaintext)
}

// newWebhookSecret generates a 32-byte random secret. Returns the raw
// base32 encoding (shown to the user once) and the AEAD ciphertext for
// persistence.
func (h *appsHandler) newWebhookSecret(appID int64) (raw string, encrypted []byte, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", nil, err
	}
	raw = strings.TrimRight(base32.StdEncoding.EncodeToString(b[:]), "=")
	encrypted, err = h.codec.Seal("webhook.secret", "app:"+strconv.FormatInt(appID, 10), []byte(raw))
	return
}

func validateGitAuthKind(k domain.GitAuthKind) error {
	switch k {
	case domain.GitAuthNone, domain.GitAuthSSH, domain.GitAuthPAT, domain.GitAuthGitHubApp:
		return nil
	default:
		return errors.New("gitAuthKind must be one of '', 'ssh', 'pat', 'github_app'")
	}
}

func (h *appsHandler) delete(w http.ResponseWriter, r *http.Request) {
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
	// Tear down running stacks + Traefik config first so the user-visible
	// teardown is complete by the time the row goes.
	if h.engine != nil {
		if err := h.engine.Teardown(r.Context(), app); err != nil {
			h.logger.Error("teardown during delete", "slug", slug, "err", err)
		}
	}
	if err := h.store.Apps.Delete(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete app")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppDelete, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "deleted app "+slug, "")
	w.WriteHeader(http.StatusNoContent)
}

type deployRequest struct {
	CommitSHA string `json:"commitSha"`
}

func (h *appsHandler) deploy(w http.ResponseWriter, r *http.Request) {
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
	if app.ComposeFile == "" && app.GitURL == "" {
		writeError(w, http.StatusBadRequest, "app has no compose source configured (paste a compose or set a git url)")
		return
	}
	var req deployRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional

	subj := auth.FromContext(r.Context())
	var triggeredBy *int64
	if subj.UserID > 0 {
		uid := subj.UserID
		triggeredBy = &uid
	}

	dep, err := h.engine.Trigger(r.Context(), app, triggeredBy, req.CommitSHA, domain.TriggerManual)
	if err != nil {
		if errors.Is(err, deploy.ErrLocked) {
			writeError(w, http.StatusConflict, "another deployment is in progress")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to trigger deploy: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionDeploymentStart, "deployment", strconv.FormatInt(dep.ID, 10),
		clientIP(r), "deploy "+slug, "")
	writeJSON(w, http.StatusAccepted, deploymentToResponse(dep))
}

func (h *appsHandler) rollback(w http.ResponseWriter, r *http.Request) {
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
	subj := auth.FromContext(r.Context())
	var triggeredBy *int64
	if subj.UserID > 0 {
		uid := subj.UserID
		triggeredBy = &uid
	}
	dep, err := h.engine.Rollback(r.Context(), app, triggeredBy)
	if err != nil {
		switch {
		case errors.Is(err, deploy.ErrLocked):
			writeError(w, http.StatusConflict, "another deployment is in progress")
		case errors.Is(err, deploy.ErrNoRollbackCandidate):
			writeError(w, http.StatusBadRequest, "no prior successful deployment to roll back to")
		default:
			writeError(w, http.StatusInternalServerError, "failed to roll back: "+err.Error())
		}
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionDeploymentRollback, "deployment", strconv.FormatInt(dep.ID, 10),
		clientIP(r), "rollback "+slug, "")
	writeJSON(w, http.StatusAccepted, deploymentToResponse(dep))
}

func (h *appsHandler) deployments(w http.ResponseWriter, r *http.Request) {
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
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit must be 1..200")
			return
		}
		limit = n
	}
	rows, err := h.store.Deployments.ListForApp(r.Context(), app.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deployments")
		return
	}
	out := make([]deploymentResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, deploymentToResponse(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// slugRe enforces our slug rules: starts with a letter, ends with a letter
// or digit, only lowercase alphanumerics and hyphens between, length 3..40.
// Slug becomes part of the Compose project name and the Traefik file path,
// so we keep the alphabet narrow.
var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

func validSlug(s string) bool {
	return slugRe.MatchString(s)
}

// splitDomainsField turns the stored comma-separated string into a slice
// for the wire format.
func splitDomainsField(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinDomains turns a slice from the wire into the stored comma-separated
// form. Empty/blank entries are dropped.
func joinDomains(in []string) string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}
