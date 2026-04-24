package traefik

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PlatformRouterOptions describes the platform UI's Traefik router. The
// settings handler regenerates this file on every ACME / HTTPS-redirect
// change so the platform UI follows the same HTTPS-only policy as
// per-app routes.
type PlatformRouterOptions struct {
	// BaseDomain is the hostname Teal serves the platform UI on (the
	// install-time TEAL_BASE_DOMAIN). Empty disables the write — there's
	// no router to render without a host rule.
	BaseDomain string

	// BackendURL is the in-cluster URL of the Teal app container. The
	// installer wires this to "http://teal:3000" via the platform_proxy
	// network.
	BackendURL string

	// TLSEnabled mirrors the per-app TLS gate: true once an ACME email
	// is configured (so a websecure cert can actually be issued).
	TLSEnabled bool

	// HTTPSRedirect adds a redirect middleware to the HTTP router. Only
	// honoured when TLSEnabled is true — redirecting to HTTPS without a
	// working cert would brick the platform UI.
	HTTPSRedirect bool
}

// WritePlatformRouter renders the platform UI's dynconf YAML to
// dir/_platform.yml. Same atomic write semantics as WriteMulti.
func WritePlatformRouter(dir string, opts PlatformRouterOptions) error {
	if opts.BaseDomain == "" {
		return fmt.Errorf("traefik: base domain required")
	}
	if opts.BackendURL == "" {
		opts.BackendURL = "http://teal:3000"
	}

	doc := dynamicFile{HTTP: httpSection{
		Routers:  map[string]router{},
		Services: map[string]service{},
	}}

	rule := renderHostRule([]string{opts.BaseDomain})
	httpRouter := router{
		Rule:        rule,
		Service:     "teal-platform",
		EntryPoints: []string{EntryPointWeb},
	}
	if opts.TLSEnabled && opts.HTTPSRedirect {
		doc.HTTP.Middlewares = map[string]middleware{
			"teal-platform-redirect": {
				RedirectScheme: &redirectScheme{Scheme: "https", Permanent: true},
			},
		}
		httpRouter.Middlewares = []string{"teal-platform-redirect"}
	}
	doc.HTTP.Routers["teal-platform"] = httpRouter

	if opts.TLSEnabled {
		doc.HTTP.Routers["teal-platform-secure"] = router{
			Rule:        rule,
			Service:     "teal-platform",
			EntryPoints: []string{EntryPointWebSecure},
			TLS:         &routerTLS{CertResolver: CertResolver},
		}
	}

	doc.HTTP.Services["teal-platform"] = service{
		LoadBalancer: loadBalancer{
			Servers: []server{{URL: opts.BackendURL}},
		},
	}

	body, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("traefik: marshal platform router: %w", err)
	}
	return atomicWrite(filepath.Join(dir, "_platform.yml"), body)
}
