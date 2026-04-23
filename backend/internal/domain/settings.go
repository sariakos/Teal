package domain

import "time"

// PlatformSetting is one row in the platform_settings KV table. Values are
// plain TEXT; consumers parse them into their own types (e.g. the Traefik
// subsystem reads "https.redirect_enabled" as a bool).
//
// Keys are namespaced with dots; the reserved namespaces in v1 are:
//   - acme.*   ACME / Let's Encrypt integration
//   - https.*  HTTPS entrypoint behaviour
type PlatformSetting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// Well-known platform setting keys. Consumers use these constants rather
// than raw strings so renames are compile-time visible.
const (
	SettingACMEEmail          = "acme.email"
	SettingACMEStaging        = "acme.staging"          // "true" → Let's Encrypt staging
	SettingHTTPSRedirect      = "https.redirect_enabled" // "true" → per-router HTTP→HTTPS redirect
)

// AppSharedEnvVarRef is one row in app_shared_env_vars — an App's explicit
// opt-in to a shared env-var key. Shared vars are never injected silently;
// the App must name each one.
type AppSharedEnvVarRef struct {
	AppID     int64
	Key       string
	CreatedAt time.Time
}
