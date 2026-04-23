package traefik

import (
	"context"

	"github.com/sariakos/teal/backend/internal/docker"
)

// PlatformNetworkName is the Docker bridge network every routed App stack
// is attached to. Hardcoded (not configurable) because users' compose files
// reference it by name via the transformation pipeline — letting operators
// rename it would break compatibility for no real benefit.
const PlatformNetworkName = "platform_proxy"

// Labels applied to the network when Teal creates it. Lets us identify it
// later (e.g. for diagnostics, refusing to manage operator-created networks
// of the same name).
var platformNetworkLabels = map[string]string{
	"teal.managed": "true",
	"teal.purpose": "platform-proxy",
}

// EnsurePlatformNetwork creates the platform_proxy network if it does not
// already exist. Idempotent. Called once at startup; the deploy engine
// assumes it exists for every deploy after that.
func EnsurePlatformNetwork(ctx context.Context, dock docker.Client) error {
	return dock.NetworkCreateIfMissing(ctx, PlatformNetworkName, platformNetworkLabels)
}
