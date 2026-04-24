package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/traefik"
)

// effectiveRoutes returns the route list the engine should render
// Traefik config for. Per-service routes (app.Routes) take precedence;
// when empty, the legacy single-domain model is preserved by
// synthesising one route covering all of `domains` to the primary
// (heuristically-picked) service.
//
// Returning an empty slice means "no routing" — the engine skips the
// Traefik flip step entirely (background-only apps).
func effectiveRoutes(app domain.App, domains []string) []domain.Route {
	if len(app.Routes) > 0 {
		return app.Routes
	}
	if len(domains) == 0 {
		return nil
	}
	// Legacy: one route, all domains share one Traefik router pointed
	// at the primary service (resolved later). Service is empty so the
	// engine knows to use the heuristic primary.
	out := make([]domain.Route, 0, len(domains))
	for _, d := range domains {
		out = append(out, domain.Route{Domain: d})
	}
	return out
}

// uniqueServiceNames returns the sorted-by-first-appearance distinct
// service names referenced by `routes`. Empty service entries (legacy
// "use the primary heuristic") are dropped — the transform's
// AttachServices is the explicit list, not the implicit primary.
func uniqueServiceNames(routes []domain.Route) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		s := strings.TrimSpace(r.Service)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// buildMultiSpec resolves each Route to a concrete (ip, port) backend
// and produces the MultiSpec the dynconf writer consumes.
//
// For each route:
//   - service name = route.Service or fall back to primaryService (the
//     transform's heuristic pick)
//   - container = looked up by (project, service)
//   - ip = container.NetworkIPs[platform_proxy]
//   - port = route.Port if set, else auto-probed via detectPort
//
// Errors at the first unresolvable route — the deploy is incoherent
// otherwise.
func (e *Engine) buildMultiSpec(
	ctx context.Context,
	app domain.App,
	dep domain.Deployment,
	routes []domain.Route,
	primaryService string,
	tlsEnabled, redirect bool,
	project string,
	logFile io.Writer,
) (traefik.MultiSpec, error) {
	spec := traefik.MultiSpec{
		Slug:          app.Slug,
		TLSEnabled:    tlsEnabled,
		HTTPSRedirect: redirect,
	}
	for _, r := range routes {
		serviceName := strings.TrimSpace(r.Service)
		if serviceName == "" {
			serviceName = primaryService
		}
		if serviceName == "" {
			return traefik.MultiSpec{}, fmt.Errorf("route %q has no service and no primary detected", r.Domain)
		}

		containerID, err := e.runner.ContainerIDByService(ctx, project, serviceName)
		if err != nil {
			return traefik.MultiSpec{}, fmt.Errorf("find container for service %q: %w", serviceName, err)
		}
		if containerID == "" {
			return traefik.MultiSpec{}, fmt.Errorf("service %q has no running container in project %q (did the deploy bring it up?)", serviceName, project)
		}
		insp, err := e.docker.ContainerInspect(ctx, containerID)
		if err != nil {
			return traefik.MultiSpec{}, fmt.Errorf("inspect %q: %w", serviceName, err)
		}
		ip := insp.NetworkIPs[traefik.PlatformNetworkName]
		if ip == "" {
			return traefik.MultiSpec{}, fmt.Errorf("service %q is not on %s — make sure its compose entry doesn't pin a different network", serviceName, traefik.PlatformNetworkName)
		}

		port, err := detectPort(ctx, ip, r.Port, func(format string, a ...any) {
			fmt.Fprintf(logFile, format, a...)
		})
		if err != nil {
			return traefik.MultiSpec{}, fmt.Errorf("port detection for %q: %w", serviceName, err)
		}

		// NamedRoute name: empty for the single-route legacy case
		// (matches the existing teal-<slug> output), service name
		// otherwise (multi-route disambiguation in the YAML).
		name := serviceName
		if len(routes) == 1 && r.Service == "" {
			name = ""
		}
		spec.Routes = append(spec.Routes, traefik.NamedRoute{
			Name:       name,
			Domains:    []string{r.Domain},
			BackendURL: fmt.Sprintf("http://%s:%d", ip, port),
		})
	}
	_ = dep // dep is reserved for future per-route audit info
	return spec, nil
}
