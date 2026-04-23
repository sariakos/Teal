// Package config loads Teal's runtime configuration from environment variables.
//
// What it does:
//   - Defines the Config struct that every other subsystem reads from.
//   - Parses environment variables, applies documented defaults, and validates them.
//
// What it does NOT do:
//   - Reload at runtime. Config is loaded once at startup; restart to change it.
//   - Read from files or remote stores. Operators wire env vars through their
//     process supervisor (docker compose, systemd) — keeping a single source of
//     truth makes the deploy story simpler and avoids precedence bugs.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration. Fields are exported so other
// packages can read them directly; treat the value as immutable after Load.
type Config struct {
	// HTTPAddr is the listen address for the HTTP API server, e.g. ":3000".
	// Env: TEAL_HTTP_ADDR. Default: ":3000".
	HTTPAddr string

	// DBPath is the filesystem path to the SQLite database file. The directory
	// is created on startup if it doesn't exist.
	// Env: TEAL_DB_PATH. Default: "./var/teal.db".
	DBPath string

	// PlatformSecret is the master secret used to derive encryption keys (env
	// var encryption, session signing, etc.). Must be at least 32 bytes.
	// Env: TEAL_PLATFORM_SECRET. No default — Teal refuses to start without it
	// in production. In dev mode (Env == "dev"), a fixed insecure value is
	// substituted so contributors don't have to set anything to run the binary.
	PlatformSecret string

	// Env names the runtime environment ("dev" or "prod"). Affects defaults
	// and whether insecure conveniences are allowed (e.g. dev auth bypass).
	// Env: TEAL_ENV. Default: "dev".
	Env string

	// LogLevel controls slog verbosity ("debug", "info", "warn", "error").
	// Env: TEAL_LOG_LEVEL. Default: "info".
	LogLevel string

	// LogFormat selects the slog handler ("text" for humans, "json" for
	// aggregators).
	// Env: TEAL_LOG_FORMAT. Default: "text" in dev, "json" in prod.
	LogFormat string

	// DockerHost is passed to the Docker SDK; empty means "use the default
	// (DOCKER_HOST env or the local socket)".
	// Env: TEAL_DOCKER_HOST. Default: "".
	DockerHost string

	// ShutdownGraceperiod bounds how long the server waits for in-flight
	// requests to drain before forcing exit on SIGINT/SIGTERM.
	// Env: TEAL_SHUTDOWN_GRACE. Default: 15s.
	ShutdownGracePeriod time.Duration

	// DevAuthBypass, when true, makes the auth middleware accept all requests.
	// Only honoured when Env == "dev". Lets us hit the API in Phase 1 before
	// real auth exists.
	// Env: TEAL_DEV_BYPASS_AUTH. Default: false.
	DevAuthBypass bool

	// WorkdirRoot is the parent directory for per-deployment working
	// directories (compose.yml, env, log per deploy). The store's SQLite
	// file lives outside this — the workdir root is exclusively engine-owned.
	// Env: TEAL_WORKDIR_ROOT. Default: "./var".
	WorkdirRoot string

	// TraefikDynamicDir is the directory the engine writes per-app dynamic
	// config YAMLs into. Traefik's file provider must be configured to
	// watch this directory. Defaults to a subpath of WorkdirRoot so a
	// single TEAL_WORKDIR_ROOT change moves everything together.
	// Env: TEAL_TRAEFIK_DYNAMIC_DIR. Default: "<WorkdirRoot>/traefik/dynamic".
	TraefikDynamicDir string

	// TraefikStaticPath is the on-disk path Teal writes Traefik's static
	// config to. The Traefik container must mount this file at
	// /etc/traefik/traefik.yml. Static config is read at boot — restart
	// Traefik to pick up changes (e.g. new ACME email).
	// Env: TEAL_TRAEFIK_STATIC_PATH. Default: "<WorkdirRoot>/traefik/traefik.yml".
	TraefikStaticPath string

	// TraefikDashboardInsecure publishes the unauthenticated Traefik
	// dashboard on :8080. Honoured only when Env == "dev" and gated by
	// validate(). Lets contributors poke at the dashboard locally.
	// Env: TEAL_TRAEFIK_DASHBOARD_INSECURE. Default: true in dev, false in prod.
	TraefikDashboardInsecure bool

	// MetricsInterval is the cadence at which the metrics scraper polls
	// `docker stats` for each platform-managed container. Floored at 5s
	// inside the scraper. Env: TEAL_METRICS_INTERVAL. Default 15s.
	MetricsInterval time.Duration

	// MetricsRetention is how long persisted metric samples (and
	// container logs in the logbuffer) survive before being pruned. Set
	// once across both subsystems so admins have a single knob.
	// Env: TEAL_METRICS_RETENTION. Default 6h.
	MetricsRetention time.Duration

	// ContainerLogsDir is where the logbuffer writes per-container
	// NDJSON files. Defaults under WorkdirRoot so a single
	// TEAL_WORKDIR_ROOT change moves everything together.
	// Env: TEAL_CONTAINER_LOGS_DIR. Default "<WorkdirRoot>/container-logs".
	ContainerLogsDir string
}

// Defaults exposes the values used when no environment variable is set. Useful
// in tests and in documentation.
func Defaults() Config {
	return Config{
		HTTPAddr:            ":3000",
		DBPath:              "./var/teal.db",
		PlatformSecret:      "",
		Env:                 "dev",
		LogLevel:            "info",
		LogFormat:           "", // resolved against Env in Load
		DockerHost:          "",
		ShutdownGracePeriod: 15 * time.Second,
		DevAuthBypass:       false,
		WorkdirRoot:         "./var",
		TraefikDynamicDir:   "", // resolved relative to WorkdirRoot in Load
		TraefikStaticPath:   "", // resolved relative to WorkdirRoot in Load
		// TraefikDashboardInsecure default is set in Load() against Env.
	}
}

// Load reads configuration from the process environment, applies defaults,
// validates the result, and returns it. An error is returned if any value is
// malformed or if a required production setting is missing.
func Load() (Config, error) {
	c := Defaults()

	c.HTTPAddr = getString("TEAL_HTTP_ADDR", c.HTTPAddr)
	c.DBPath = getString("TEAL_DB_PATH", c.DBPath)
	c.PlatformSecret = getString("TEAL_PLATFORM_SECRET", c.PlatformSecret)
	c.Env = strings.ToLower(getString("TEAL_ENV", c.Env))
	c.LogLevel = strings.ToLower(getString("TEAL_LOG_LEVEL", c.LogLevel))
	c.LogFormat = strings.ToLower(getString("TEAL_LOG_FORMAT", c.LogFormat))
	c.DockerHost = getString("TEAL_DOCKER_HOST", c.DockerHost)
	c.WorkdirRoot = getString("TEAL_WORKDIR_ROOT", c.WorkdirRoot)
	c.TraefikDynamicDir = getString("TEAL_TRAEFIK_DYNAMIC_DIR", c.TraefikDynamicDir)
	c.TraefikStaticPath = getString("TEAL_TRAEFIK_STATIC_PATH", c.TraefikStaticPath)
	c.ContainerLogsDir = getString("TEAL_CONTAINER_LOGS_DIR", c.ContainerLogsDir)

	mi, err := getDuration("TEAL_METRICS_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	c.MetricsInterval = mi
	mr, err := getDuration("TEAL_METRICS_RETENTION", 6*time.Hour)
	if err != nil {
		return Config{}, err
	}
	c.MetricsRetention = mr

	d, err := getDuration("TEAL_SHUTDOWN_GRACE", c.ShutdownGracePeriod)
	if err != nil {
		return Config{}, err
	}
	c.ShutdownGracePeriod = d

	b, err := getBool("TEAL_DEV_BYPASS_AUTH", c.DevAuthBypass)
	if err != nil {
		return Config{}, err
	}
	c.DevAuthBypass = b

	// Resolve log format default after Env is known: humans want text in dev,
	// log aggregators want JSON in prod.
	if c.LogFormat == "" {
		if c.Env == "prod" {
			c.LogFormat = "json"
		} else {
			c.LogFormat = "text"
		}
	}

	// In dev, fall back to a fixed insecure secret so the binary boots out of
	// the box. Refuse in prod — the platform secret protects encryption keys,
	// so booting without one would silently degrade security.
	if c.PlatformSecret == "" {
		if c.Env == "dev" {
			c.PlatformSecret = "dev-insecure-secret-do-not-use-in-prod-please"
		} else {
			return Config{}, errors.New("TEAL_PLATFORM_SECRET is required when TEAL_ENV != dev")
		}
	}

	if c.TraefikDynamicDir == "" {
		c.TraefikDynamicDir = c.WorkdirRoot + "/traefik/dynamic"
	}
	if c.TraefikStaticPath == "" {
		c.TraefikStaticPath = c.WorkdirRoot + "/traefik/traefik.yml"
	}
	if c.ContainerLogsDir == "" {
		c.ContainerLogsDir = c.WorkdirRoot + "/container-logs"
	}

	dashInsecure, err := getBool("TEAL_TRAEFIK_DASHBOARD_INSECURE", c.Env == "dev")
	if err != nil {
		return Config{}, err
	}
	c.TraefikDashboardInsecure = dashInsecure

	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) validate() error {
	if len(c.PlatformSecret) < 32 {
		return fmt.Errorf("TEAL_PLATFORM_SECRET must be at least 32 bytes (got %d)", len(c.PlatformSecret))
	}
	switch c.Env {
	case "dev", "prod":
	default:
		return fmt.Errorf("TEAL_ENV must be 'dev' or 'prod' (got %q)", c.Env)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("TEAL_LOG_LEVEL must be one of debug|info|warn|error (got %q)", c.LogLevel)
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("TEAL_LOG_FORMAT must be 'text' or 'json' (got %q)", c.LogFormat)
	}
	if c.HTTPAddr == "" {
		return errors.New("TEAL_HTTP_ADDR must not be empty")
	}
	if c.DBPath == "" {
		return errors.New("TEAL_DB_PATH must not be empty")
	}
	if c.ShutdownGracePeriod <= 0 {
		return fmt.Errorf("TEAL_SHUTDOWN_GRACE must be positive (got %s)", c.ShutdownGracePeriod)
	}
	if c.DevAuthBypass && c.Env != "dev" {
		return errors.New("TEAL_DEV_BYPASS_AUTH=true is only allowed when TEAL_ENV=dev")
	}
	if c.WorkdirRoot == "" {
		return errors.New("TEAL_WORKDIR_ROOT must not be empty")
	}
	if c.TraefikDynamicDir == "" {
		return errors.New("TEAL_TRAEFIK_DYNAMIC_DIR must not be empty")
	}
	if c.TraefikStaticPath == "" {
		return errors.New("TEAL_TRAEFIK_STATIC_PATH must not be empty")
	}
	if c.TraefikDashboardInsecure && c.Env != "dev" {
		return errors.New("TEAL_TRAEFIK_DASHBOARD_INSECURE=true is only allowed when TEAL_ENV=dev")
	}
	if c.MetricsInterval < time.Second {
		return fmt.Errorf("TEAL_METRICS_INTERVAL must be >= 1s (got %s)", c.MetricsInterval)
	}
	if c.MetricsRetention < time.Minute {
		return fmt.Errorf("TEAL_METRICS_RETENTION must be >= 1m (got %s)", c.MetricsRetention)
	}
	if c.ContainerLogsDir == "" {
		return errors.New("TEAL_CONTAINER_LOGS_DIR must not be empty")
	}
	return nil
}

func getString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func getBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}
