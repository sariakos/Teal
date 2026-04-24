package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/auth"
	apimw "github.com/sariakos/teal/backend/internal/api/middleware"
	"github.com/sariakos/teal/backend/internal/containerwatcher"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/logbuffer"
	"github.com/sariakos/teal/backend/internal/realtime"
	"github.com/sariakos/teal/backend/internal/store"
)

// Deps bundles every external collaborator the API needs. Callers (cmd/teal)
// construct one and hand it to NewServer; the API package never reaches out
// to fetch dependencies on its own.
type Deps struct {
	Logger         *slog.Logger
	Store          *store.Store
	Docker         docker.Client
	Authenticator  *auth.Authenticator
	RateLimiter    *auth.LoginRateLimiter
	Engine         *deploy.Engine
	Codec          *crypto.Codec // shared crypto primitive for git/webhook secrets
	Hub            *realtime.Hub // pub/sub fanout for WS subscribers; nil disables /ws
	LogBuffer      *logbuffer.Registry           // optional: enables container log replay endpoint
	ContainerWatch *containerwatcher.Watcher     // optional: enables /apps/{slug}/containers
	WorkdirRoot    string                        // for the deploy.log streaming endpoint
	DevCORSOrigins []string      // empty disables CORS (prod default)

	// TraefikStaticPath is the on-disk path for Traefik's static config.
	// When non-empty the settings handler regenerates the file after every
	// successful change. cmd/teal supplies it; the API integration tests
	// leave it empty (no static config to write in unit tests).
	TraefikStaticPath        string
	TraefikDashboardInsecure bool

	// TraefikDynamicDir is the directory the engine writes per-app
	// dynconf files to. The settings handler also uses it to (re)write
	// the platform UI's _platform.yml whenever HTTPS settings change.
	TraefikDynamicDir string

	// BaseDomain is the operator-set TEAL_BASE_DOMAIN. The settings
	// handler uses it as the Host rule for the platform UI router.
	// Empty disables platform-router regeneration (the file the
	// installer wrote stays put).
	BaseDomain string

	// GitHub App: shared installation-token cache, the platform secret
	// (used to HMAC-sign install-flow state), and the public base URL
	// surfaced back to the UI as the callback hint. cmd/teal sets all
	// three; tests can leave them zero (the install flow is then
	// disabled).
	GitHubAppTokenCache *githubapp.TokenCache
	StateSecret         []byte
	PublicBaseURL       string
}

// NewServer returns an *http.Server with the Teal router configured.
func NewServer(addr string, d Deps) *http.Server {
	r := newRouter(d)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func newRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(apimw.Recover(d.Logger))
	r.Use(apimw.RequestID)
	r.Use(apimw.AccessLog(d.Logger))
	if len(d.DevCORSOrigins) > 0 {
		r.Use(apimw.DevCORS(d.DevCORSOrigins))
	}

	r.Get("/healthz", Health)

	authH := &authHandler{
		logger: d.Logger, store: d.Store, authn: d.Authenticator, rateLimiter: d.RateLimiter,
	}
	usersH := &usersHandler{logger: d.Logger, store: d.Store}
	apiKeysH := &apiKeysHandler{logger: d.Logger, store: d.Store, mgr: d.Authenticator.APIKeys}
	auditH := &auditHandler{logs: d.Store.AuditLogs}
	dockerH := &dockerHandler{logger: d.Logger, store: d.Store, docker: d.Docker}
	appsH := &appsHandler{logger: d.Logger, store: d.Store, engine: d.Engine, codec: d.Codec}
	depsH := &deploymentsHandler{deployments: d.Store.Deployments, engine: d.Engine}
	webhookH := &webhookHandler{logger: d.Logger, store: d.Store, codec: d.Codec, engine: d.Engine}
	envH := &envVarsHandler{logger: d.Logger, store: d.Store, codec: d.Codec}
	sharedEnvH := &sharedEnvVarsHandler{logger: d.Logger, store: d.Store, codec: d.Codec}
	settingsH := &settingsHandler{
		logger:             d.Logger,
		store:              d.Store,
		traefikStaticPath:  d.TraefikStaticPath,
		traefikDynamicDir:  d.TraefikDynamicDir,
		baseDomain:         d.BaseDomain,
		dashboardInsecure:  d.TraefikDashboardInsecure,
	}
	wsH := &wsHandler{logger: d.Logger, hub: d.Hub, allowedOrigins: d.DevCORSOrigins}
	ghAppH := &gitHubAppHandler{
		logger: d.Logger, store: d.Store, codec: d.Codec,
		tokenCache:    d.GitHubAppTokenCache,
		stateSecret:   d.StateSecret,
		publicBaseURL: d.PublicBaseURL,
	}
	ghAppWH := &gitHubAppWebhookHandler{
		logger: d.Logger, store: d.Store, codec: d.Codec, engine: d.Engine,
	}
	ghAppAdminH := &gitHubAppAdminHandler{
		logger: d.Logger, store: d.Store, codec: d.Codec,
	}
	logsH := &logsHandler{logger: d.Logger, store: d.Store, logbuf: d.LogBuffer, watcher: d.ContainerWatch, workdirRoot: d.WorkdirRoot}
	servicesH := &servicesHandler{logger: d.Logger, store: d.Store, workdirRoot: d.WorkdirRoot}
	metricsH := &metricsHandler{logger: d.Logger, store: d.Store}
	notifH := &notificationsHandler{logger: d.Logger, store: d.Store}
	platformH := &platformHandler{
		logger: d.Logger, store: d.Store, docker: d.Docker,
		watcher: d.ContainerWatch, workdirRoot: d.WorkdirRoot,
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(apimw.JSONResponse)

		// Unauthenticated endpoints — login, bootstrap, setup status,
		// GitHub webhooks (HMAC-authenticated by the handler).
		r.Get("/setup-status", authH.setupStatus)
		r.Post("/login", authH.login)
		r.Post("/register-bootstrap", authH.registerBootstrap)
		r.Post("/webhooks/github/{slug}", webhookH.handle)

		// GitHub App centralized webhook. One URL for all installations
		// of the platform's App; routes by repo full name. HMAC against
		// the platform-wide webhook secret.
		r.Post("/webhooks/github-app", ghAppWH.handle)

		// GitHub App install callback. GitHub redirects the user's
		// browser here after they pick which repos to install on; the
		// handler verifies the HMAC-signed state and stores the
		// linkage. Unauth because GitHub is the caller — the state
		// signature is the bearer of authority.
		r.Get("/github-app/setup-callback", ghAppH.setupCallback)

		// Authenticated endpoints. auth → CSRF order matters (CSRF needs
		// the session attached by auth).
		r.Group(func(r chi.Router) {
			r.Use(d.Authenticator.Middleware())
			r.Use(auth.CSRFMiddleware)

			r.Get("/me", authH.me)
			r.Post("/logout", authH.logout)

			// WebSocket endpoint. The handshake itself is a GET so it
			// doesn't trip CSRF; once upgraded, all messages are
			// app-level (subscribe/unsubscribe) and the session cookie
			// already authorised the connection.
			if d.Hub != nil {
				r.Get("/ws", wsH.handle)
			}

			// App resources. List + GET are open to viewer; mutations require
			// member or above.
			r.Get("/apps", appsH.list)
			r.Get("/apps/{slug}", appsH.get)
			r.Get("/apps/{slug}/deployments", appsH.deployments)
			r.Get("/apps/{slug}/deployments/{id}/log", logsH.deploymentLog)
			r.Get("/apps/{slug}/containers", logsH.listContainers)
			r.Get("/apps/{slug}/services", servicesH.list)
			r.Get("/apps/{slug}/metrics", metricsH.list)
			r.Get("/containers/{id}/logs", logsH.containerLogs)
			r.With(auth.RequireRole(domain.UserRoleMember)).Group(func(r chi.Router) {
				r.Post("/apps", appsH.create)
				r.Patch("/apps/{slug}", appsH.update)
				r.Delete("/apps/{slug}", appsH.delete)
				r.Post("/apps/{slug}/deploy", appsH.deploy)
				r.Post("/apps/{slug}/rollback", appsH.rollback)
				r.Get("/apps/{slug}/deploy-key", appsH.deployKey)
				r.Post("/apps/{slug}/rotate-deploy-key", appsH.rotateDeployKey)
				r.Post("/apps/{slug}/rotate-webhook-secret", appsH.rotateWebhookSecret)
				r.Post("/apps/{slug}/rotate-notification-secret", appsH.rotateNotificationSecret)
				r.Post("/apps/{slug}/install-github-app", ghAppH.startInstall)

				r.Get("/apps/{slug}/envvars", envH.listApp)
				r.Post("/apps/{slug}/envvars", envH.upsertApp)
				r.Delete("/apps/{slug}/envvars/{key}", envH.deleteApp)
				r.Get("/apps/{slug}/shared-envvars", sharedEnvH.listAppShared)
				r.Put("/apps/{slug}/shared-envvars", sharedEnvH.setAppShared)
			})

			r.Get("/deployments", depsH.list)
			r.Get("/deployments/{id}", depsH.get)

			r.Get("/notifications", notifH.list)
			r.Post("/notifications/{id}/read", notifH.markRead)
			r.Post("/notifications/read-all", notifH.markAllRead)

			r.Get("/platform/summary", platformH.summary)

			r.Get("/apikeys", apiKeysH.list)
			r.Post("/apikeys", apiKeysH.create)
			r.Delete("/apikeys/{id}", apiKeysH.revoke)

			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(domain.UserRoleAdmin))
				r.Get("/users", usersH.list)
				r.Post("/users", usersH.create)
				r.Patch("/users/{id}", usersH.update)
				r.Delete("/users/{id}", usersH.delete)
				r.Get("/audit-logs", auditH.list)

				r.Get("/shared-envvars", sharedEnvH.listShared)
				r.Post("/shared-envvars", sharedEnvH.upsertShared)
				r.Delete("/shared-envvars/{key}", sharedEnvH.deleteShared)

				r.Get("/settings", settingsH.list)
				r.Put("/settings/{key}", settingsH.upsert)
				r.Delete("/settings/{key}", settingsH.delete)

				r.Get("/settings/github-app", ghAppAdminH.get)
				r.Put("/settings/github-app", ghAppAdminH.put)

				r.Delete("/docker/volumes/{name}", dockerH.deleteVolume)

				r.Post("/platform/self-update", platformH.selfUpdate)
			})

			r.Route("/docker", func(r chi.Router) {
				r.Get("/containers", dockerH.listContainers)
				r.Get("/networks", dockerH.listNetworks)
				r.Get("/volumes", dockerH.listVolumes)
			})
		})
	})

	registerFrontend(r)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	return r
}

func Shutdown(ctx context.Context, srv *http.Server, grace time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
