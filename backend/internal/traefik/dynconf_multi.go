package traefik

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MultiSpec describes an app's per-route Traefik config. Each Route
// becomes its own router/service pair in the dynconf YAML — this is
// how a single app exposes multiple services on different domains
// (e.g. app.example.com → web container, api.example.com → api
// container).
//
// MultiSpec replaces RouterSpec for the per-service routing flow. The
// legacy single-domain RouterSpec/Write is kept around for back-compat
// during the rollout; the engine calls WriteMulti when the app has
// per-service routes configured.
type MultiSpec struct {
	Slug          string
	Routes        []NamedRoute
	TLSEnabled    bool
	HTTPSRedirect bool
}

// NamedRoute is one router + one service in the dynconf. Name must be
// unique within the file (the engine uses the compose service name);
// empty Name maps to no suffix in the router/service identifier so a
// single-route MultiSpec produces the same output shape as the legacy
// single-domain Write.
type NamedRoute struct {
	Name       string
	Domains    []string
	BackendURL string
}

// WriteMulti renders + atomically writes the per-app dynconf file.
// Same write semantics (tmp + fsync + rename) as Write so Traefik's
// file watcher sees a complete file or nothing.
func WriteMulti(dir string, spec MultiSpec) error {
	if spec.Slug == "" {
		return fmt.Errorf("traefik: slug is required")
	}
	if len(spec.Routes) == 0 {
		return fmt.Errorf("traefik: at least one route required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("traefik: ensure dir: %w", err)
	}

	doc := dynamicFile{HTTP: httpSection{
		Routers:  map[string]router{},
		Services: map[string]service{},
	}}
	if spec.HTTPSRedirect && spec.TLSEnabled {
		doc.HTTP.Middlewares = map[string]middleware{}
	}

	for _, r := range spec.Routes {
		if len(r.Domains) == 0 {
			return fmt.Errorf("traefik: route has no domains")
		}
		if r.BackendURL == "" {
			return fmt.Errorf("traefik: route %q has no backend URL", r.Name)
		}
		base := "teal-" + spec.Slug
		if r.Name != "" {
			base = base + "-" + r.Name
		}
		rule := renderHostRule(r.Domains)

		httpRouter := router{
			Rule:        rule,
			Service:     base,
			EntryPoints: []string{EntryPointWeb},
		}
		if spec.HTTPSRedirect && spec.TLSEnabled {
			redirectName := base + "-redirect"
			doc.HTTP.Middlewares[redirectName] = middleware{
				RedirectScheme: &redirectScheme{Scheme: "https", Permanent: true},
			}
			httpRouter.Middlewares = []string{redirectName}
		}
		doc.HTTP.Routers[base] = httpRouter

		if spec.TLSEnabled {
			doc.HTTP.Routers[base+"-secure"] = router{
				Rule:        rule,
				Service:     base,
				EntryPoints: []string{EntryPointWebSecure},
				TLS:         &routerTLS{CertResolver: CertResolver},
			}
		}

		doc.HTTP.Services[base] = service{
			LoadBalancer: loadBalancer{
				Servers: []server{{URL: r.BackendURL}},
			},
		}
	}

	body, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("traefik: marshal: %w", err)
	}
	return atomicWrite(Path(dir, spec.Slug), body)
}

// atomicWrite writes data to path via a sibling tmp file + rename so
// Traefik's file watcher sees a complete file or nothing. Extracted
// from Write so WriteMulti shares the same I/O semantics.
func atomicWrite(target string, data []byte) error {
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("traefik: create tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("traefik: write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("traefik: fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("traefik: close tmp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("traefik: rename: %w", err)
	}
	return nil
}
