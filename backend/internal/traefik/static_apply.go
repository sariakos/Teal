package traefik

import (
	"context"
	"fmt"

	"github.com/sariakos/teal/backend/internal/domain"
)

// SettingsReader is the read surface ApplyStaticFromSettings needs. Both
// the live PlatformSettingsRepo and a test fake satisfy this interface
// without dragging in the rest of the store package.
type SettingsReader interface {
	GetOrDefault(ctx context.Context, key, def string) (string, error)
}

// ApplyStaticFromSettings reads ACME-related settings and writes the
// static Traefik config to staticPath. Idempotent: writes happen even
// when no settings are configured (so the file always exists on disk and
// Traefik has a complete static config to boot against).
//
// dashboardInsecure mirrors cfg.TraefikDashboardInsecure — derived from
// process config, not platform settings, because it's an operator-level
// decision rather than a platform-runtime tunable.
func ApplyStaticFromSettings(ctx context.Context, settings SettingsReader, staticPath string, dashboardInsecure bool) error {
	email, err := settings.GetOrDefault(ctx, domain.SettingACMEEmail, "")
	if err != nil {
		return fmt.Errorf("read acme email: %w", err)
	}
	stagingStr, err := settings.GetOrDefault(ctx, domain.SettingACMEStaging, "false")
	if err != nil {
		return fmt.Errorf("read acme staging: %w", err)
	}
	return WriteStatic(staticPath, StaticOptions{
		ACMEEmail:         email,
		ACMEStaging:       stagingStr == "true",
		DashboardInsecure: dashboardInsecure,
	})
}
