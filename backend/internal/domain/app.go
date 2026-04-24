package domain

import "time"

// AppStatus is the high-level state shown in the dashboard for an App. It is
// derived from Deployments and container state — it is stored on the App row
// for fast listing rather than recomputed on every read.
type AppStatus string

const (
	AppStatusIdle      AppStatus = "idle"
	AppStatusDeploying AppStatus = "deploying"
	AppStatusRunning   AppStatus = "running"
	AppStatusFailed    AppStatus = "failed"
	AppStatusStopped   AppStatus = "stopped"
)

// App is one docker-compose project managed by Teal. Each App produces a
// stream of Deployments over its lifetime; at most one Deployment per App
// runs concurrently (enforced by the deployment locking mechanism in
// Phase 3).
//
// Identity rules:
//   - ID is the stable internal identifier (used in foreign keys).
//   - Slug is the URL-safe human handle (used in Compose project names like
//     "<slug>-blue") and must be unique across all Apps.
type App struct {
	ID   int64
	Slug string
	Name string

	// ComposeFile is the raw user-supplied docker-compose.yml. Stored as text
	// so we can re-transform it on every deploy without re-parsing user input
	// from a working tree. Empty until Phase 3 wires the editor.
	ComposeFile string

	// AutoDeployBranch is the git branch that triggers a deploy when a
	// matching webhook arrives. Empty means no auto-deploy is configured yet.
	AutoDeployBranch string

	// AutoDeployEnabled toggles auto-deploy without losing the branch
	// configuration. See spec §3 — "auto-deploy toggle".
	AutoDeployEnabled bool

	// Domains is the comma-separated list of hostnames Traefik routes to
	// this App. Order is not significant. Empty until configured.
	Domains string

	// ActiveColor is the Color currently receiving traffic (the one Traefik's
	// dynamic config points at). Empty until the first successful deploy.
	// The next Deployment targets the opposite color.
	ActiveColor Color

	// QueueDeploys is reserved. v1 always rejects concurrent deploy attempts
	// with 409 Conflict; flipping this to true would let the engine queue
	// instead. Schema-only in Phase 3.
	QueueDeploys bool

	// GitURL is the clone URL (https or ssh). Empty when git is not
	// configured. When set, the engine reads compose from the repo at
	// deploy time and ignores the stored ComposeFile.
	GitURL string

	// GitAuthKind is "" (public repo), "ssh" (deploy key), or "pat"
	// (personal access token). Same enum as the schema CHECK.
	GitAuthKind GitAuthKind

	// GitAuthCredentialEncrypted is AES-GCM ciphertext of the SSH private
	// key PEM (when GitAuthKind == "ssh") or the PAT raw token (when "pat").
	// Decrypted only inside the engine's clone path.
	GitAuthCredentialEncrypted []byte

	// GitBranch is the explicit branch to clone. When empty, the engine
	// falls back to AutoDeployBranch. Webhook matching also uses the
	// effective branch (Git override → AutoDeployBranch fallback).
	GitBranch string

	// GitComposePath is the relative path to the compose file inside the
	// repo. Defaults to "docker-compose.yml".
	GitComposePath string

	// WebhookSecretEncrypted is AES-GCM ciphertext of the HMAC secret used
	// to validate inbound GitHub webhooks for this app. Generated server-
	// side on first git-source save and shown to the user exactly once.
	WebhookSecretEncrypted []byte

	// LastDeployedCommitSHA is denormalised from the most recently succeeded
	// deployment. Updated by the engine on success. Empty until first
	// successful deploy.
	LastDeployedCommitSHA string

	// CPULimit and MemoryLimit are Compose-style strings ("0.5", "512m").
	// Empty disables the limit. Injected into deploy.resources.limits.
	CPULimit    string
	MemoryLimit string

	// NotificationWebhookURL receives a JSON POST on every terminal
	// deployment (success and failure). Empty disables. Body is signed
	// with the per-app webhook secret stored in
	// NotificationWebhookSecretEncrypted.
	NotificationWebhookURL                string
	NotificationWebhookSecretEncrypted    []byte

	// NotificationEmail receives an email on deploy failure (only).
	// Requires platform-wide SMTP configured. Empty disables.
	NotificationEmail string

	// GitHubAppInstallationID identifies which install of the platform-
	// wide GitHub App can clone this app's repo. 0 when none. Only
	// meaningful when GitAuthKind == "github_app".
	GitHubAppInstallationID int64

	// GitHubAppRepo is the "owner/repo" full name picked at install
	// time. Used by the centralized webhook to route push events back
	// to this app.
	GitHubAppRepo string

	// Routes is the per-service routing config — one entry per
	// Traefik router we should write. Replaces the all-domains-go-to-
	// the-primary-service behaviour of `Domains`. When empty, the
	// engine falls back to Domains for backwards compatibility.
	Routes []Route

	Status AppStatus

	CreatedAt time.Time
	UpdatedAt time.Time
}

// GitAuthKind is the small enum stored in apps.git_auth_kind.
type GitAuthKind string

const (
	GitAuthNone      GitAuthKind = ""
	GitAuthSSH       GitAuthKind = "ssh"
	GitAuthPAT       GitAuthKind = "pat"
	GitAuthGitHubApp GitAuthKind = "github_app"
)

// EffectiveGitBranch returns the branch the engine should clone / the
// webhook handler should match against. GitBranch takes precedence so a
// per-source override is possible without disturbing AutoDeployBranch.
func (a App) EffectiveGitBranch() string {
	if a.GitBranch != "" {
		return a.GitBranch
	}
	return a.AutoDeployBranch
}

// Route is one Traefik router config for an App: a single domain
// pointed at a specific service in the compose. The engine writes
// one router (HTTP + HTTPS when TLS is enabled) per Route.
//
// All fields except Domain are optional:
//   - Service: when empty, the engine picks the "primary" service via
//     heuristics (first with ports:, or labelled teal.primary). Use
//     this when you don't care or have one service.
//   - Port: when 0, the engine probes the container for a listening
//     HTTP port (see deploy.CommonHTTPPorts). Override only when the
//     probe gets the wrong port.
type Route struct {
	Service string `json:"service,omitempty"`
	Domain  string `json:"domain"`
	Port    int    `json:"port,omitempty"`
}
