// Command teal is the single binary that runs the Teal platform.
//
// Responsibilities (and ONLY these):
//
//  1. Load configuration.
//  2. Construct the logger.
//  3. Open the SQLite store (which runs migrations on Open).
//  4. Connect to Docker.
//  5. Construct auth components (session manager, API-key manager,
//     rate limiter, Authenticator).
//  6. Start the HTTP server.
//  7. Run a periodic session-cleanup goroutine.
//  8. Wait for SIGINT/SIGTERM and shut everything down in reverse order.
//
// There is no business logic here. If you find yourself reaching for a
// helper function in this file, it probably belongs in one of the internal
// packages.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sariakos/teal/backend/internal/api"
	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/config"
	"github.com/sariakos/teal/backend/internal/containerwatcher"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/logbuffer"
	"github.com/sariakos/teal/backend/internal/logging"
	"github.com/sariakos/teal/backend/internal/metrics"
	"github.com/sariakos/teal/backend/internal/notify"
	"github.com/sariakos/teal/backend/internal/realtime"
	"github.com/sariakos/teal/backend/internal/store"
	"github.com/sariakos/teal/backend/internal/traefik"
)

// notifyAdapter bridges notify.Notifier (which has its own Event type)
// to deploy.Notifier (interface using deploy.NotifyEvent). Defined here
// so both packages stay free of import cycles.
type notifyAdapter struct{ inner *notify.Notifier }

func (a notifyAdapter) OnDeploymentFinished(ctx context.Context, evt deploy.NotifyEvent) {
	a.inner.OnDeploymentFinished(ctx, notify.Event{
		App: evt.App, Deployment: evt.Deployment, Failed: evt.Failed, Reason: evt.Reason,
	})
}

const (
	loginRateLimitCapacity = 5
	loginRateLimitWindow   = 1 * time.Minute
	sessionSweepInterval   = 5 * time.Minute
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "teal: fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := logging.New(cfg.LogFormat, cfg.LogLevel)
	logger.Info("starting teal", "env", cfg.Env, "addr", cfg.HTTPAddr, "db", cfg.DBPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("store close", "err", err)
		}
	}()

	dock, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer func() {
		if err := dock.Close(); err != nil {
			logger.Error("docker close", "err", err)
		}
	}()
	if err := dock.Ping(ctx); err != nil {
		logger.Warn("docker ping failed at startup; continuing", "err", err)
	}

	// Ensure platform_proxy network exists. Best-effort: if Docker is down
	// we logged the warning above; the engine will surface a real error
	// the first time it tries to deploy.
	if err := traefik.EnsurePlatformNetwork(ctx, dock); err != nil {
		logger.Warn("ensure platform_proxy network failed; continuing", "err", err)
	}

	// Share one Codec between the engine and API — they encrypt/decrypt the
	// same app-bound secrets (git credentials, webhook secret).
	codec, err := crypto.NewCodec([]byte(cfg.PlatformSecret))
	if err != nil {
		return fmt.Errorf("crypto codec: %w", err)
	}
	engine := deploy.NewWithCodec(logger, st, dock, deploy.EngineConfig{
		WorkdirRoot:       cfg.WorkdirRoot,
		TraefikDynamicDir: cfg.TraefikDynamicDir,
	}, codec)

	// Realtime hub + producers wired in dependency order:
	//   container watcher discovers platform containers
	//   → metrics scraper + logbuffer registry react to start/stop
	//   → engine + scraper + logbuffer publish on the hub
	//   → /ws fans out to subscribers.
	hub := realtime.NewHub(logger)
	engine.SetPublisher(hub)

	notifier := notify.New(logger, st, codec, hub)
	engine.SetNotifier(notifyAdapter{inner: notifier})

	// GitHub App installation-token cache. Process-local — restart
	// wipes it (a few extra mints, no security concern). Shared with
	// the API in commit 2 (install-flow callback).
	ghAppTokens := githubapp.NewTokenCache(nil)
	engine.SetGitHubAppTokenCache(ghAppTokens)

	watcher := containerwatcher.New(logger, dock, 0) // default 2s
	scraper := metrics.New(logger, dock, st.Metrics, hub, metrics.Config{
		Interval:  cfg.MetricsInterval,
		Retention: cfg.MetricsRetention,
	})
	downsampler := metrics.NewDownsampler(logger, st.Metrics, metrics.DownsampleConfig{
		RawRetention:   cfg.MetricsRetention,
		TotalRetention: 24 * time.Hour,
	})
	logRegistry := logbuffer.NewRegistry(logger, dock, hub, logbuffer.Config{
		Root:      cfg.ContainerLogsDir,
		Retention: cfg.MetricsRetention,
	})
	watcher.Subscribe(scraper)
	watcher.Subscribe(logRegistry)

	rtDone := startRealtime(ctx, logger, watcher, scraper, logRegistry, downsampler)

	// Write the Traefik static config at boot from current platform
	// settings. The file always exists so the Traefik container can boot;
	// admins later edit settings + restart Traefik to enable HTTPS.
	if err := traefik.ApplyStaticFromSettings(ctx, st.PlatformSettings, cfg.TraefikStaticPath, cfg.TraefikDashboardInsecure); err != nil {
		logger.Warn("write traefik static config", "err", err)
	}

	authn := &auth.Authenticator{
		Sessions:  auth.NewSessionManager(st.Sessions, cfg.Env == "prod"),
		APIKeys:   auth.NewAPIKeyManager(st.APIKeys),
		Users:     st.Users,
		DevBypass: cfg.DevAuthBypass,
	}
	rateLimiter := auth.NewLoginRateLimiter(loginRateLimitCapacity, loginRateLimitWindow)

	// Public base URL for the GitHub App install callback hint.
	// Operator-set TEAL_BASE_DOMAIN (the installer writes this);
	// derived as https://<domain> when present, otherwise empty (the
	// callback URL is just a UI hint — its absence doesn't break the
	// flow, the user can compose the URL themselves).
	publicBaseURL := ""
	if d := os.Getenv("TEAL_BASE_DOMAIN"); d != "" {
		publicBaseURL = "https://" + d
	}

	deps := api.Deps{
		Logger:                   logger,
		Store:                    st,
		Docker:                   dock,
		Authenticator:            authn,
		RateLimiter:              rateLimiter,
		Engine:                   engine,
		Codec:                    codec,
		Hub:                      hub,
		LogBuffer:                logRegistry,
		ContainerWatch:           watcher,
		WorkdirRoot:              cfg.WorkdirRoot,
		TraefikStaticPath:        cfg.TraefikStaticPath,
		TraefikDashboardInsecure: cfg.TraefikDashboardInsecure,
		TraefikDynamicDir:        cfg.TraefikDynamicDir,
		BaseDomain:               os.Getenv("TEAL_BASE_DOMAIN"),
		GitHubAppTokenCache:      ghAppTokens,
		StateSecret:              []byte(cfg.PlatformSecret),
		PublicBaseURL:            publicBaseURL,
	}
	if cfg.Env == "dev" {
		// Allow the SvelteKit dev server to call us with credentials.
		deps.DevCORSOrigins = []string{"http://localhost:5173"}
	}
	srv := api.NewServer(cfg.HTTPAddr, deps)

	sweepDone := startSessionSweeper(ctx, logger, st)

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	}

	if err := api.Shutdown(context.Background(), srv, cfg.ShutdownGracePeriod); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		return err
	}
	<-sweepDone
	<-rtDone
	logger.Info("shutdown complete")
	return nil
}

// startRealtime launches every realtime/metrics goroutine bound to
// ctx. Returns a channel closed once all four have exited cleanly.
func startRealtime(ctx context.Context, logger *slog.Logger,
	watcher *containerwatcher.Watcher,
	scraper *metrics.Scraper,
	logReg *logbuffer.Registry,
	downsampler *metrics.Downsampler,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		errCh := make(chan error, 4)
		go func() { errCh <- watcher.Run(ctx) }()
		go func() { errCh <- scraper.Run(ctx) }()
		go func() { errCh <- logReg.Run(ctx) }()
		go func() { errCh <- downsampler.Run(ctx) }()
		for i := 0; i < 4; i++ {
			if err := <-errCh; err != nil {
				logger.Warn("realtime subsystem exited with error", "err", err)
			}
		}
	}()
	return done
}

// startSessionSweeper runs a goroutine that periodically deletes expired
// sessions. Returns a channel that is closed when the goroutine exits, so
// main can wait for clean shutdown.
func startSessionSweeper(ctx context.Context, logger *slog.Logger, st *store.Store) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(sessionSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := st.Sessions.DeleteExpired(context.Background(), time.Now().UTC())
				if err != nil {
					logger.Error("session sweep failed", "err", err)
					continue
				}
				if n > 0 {
					logger.Info("session sweep removed expired sessions", "count", n)
				}
			}
		}
	}()
	return done
}
