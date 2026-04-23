package traefik

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EntryPoint is the legacy alias for the HTTP entrypoint. Phase 5 uses
// EntryPointWeb (defined in static.go) directly; this constant remains
// for backwards compatibility within this package.
const EntryPoint = EntryPointWeb

// RouterSpec describes one App's Traefik router + service. The Slug is
// used to derive the router/service name (and the file path on disk).
// Domains must be non-empty.
//
// TLS fields control the Phase 5 HTTPS wiring:
//   - TLSEnabled emits a second router on the websecure entrypoint with
//     a tls block referencing the platform's certresolver.
//   - HTTPSRedirect adds a redirect middleware to the HTTP router so
//     plain-HTTP requests get a 308 to https://. Only meaningful when
//     TLSEnabled is also true.
type RouterSpec struct {
	Slug          string
	Domains       []string
	BackendURL    string // full URL Traefik will proxy to, e.g. "http://172.18.0.5:80"
	TLSEnabled    bool
	HTTPSRedirect bool
}

// dynamicFile is the wire shape of one per-app YAML file. Traefik's file
// provider merges every file in the watched directory into a single config,
// so multiple apps coexist without overlap as long as router/service names
// differ — which they do, because we name them after the app slug.
type dynamicFile struct {
	HTTP httpSection `yaml:"http"`
}

type httpSection struct {
	Routers     map[string]router     `yaml:"routers"`
	Services    map[string]service    `yaml:"services"`
	Middlewares map[string]middleware `yaml:"middlewares,omitempty"`
}

type router struct {
	Rule        string     `yaml:"rule"`
	Service     string     `yaml:"service"`
	EntryPoints []string   `yaml:"entryPoints"`
	Middlewares []string   `yaml:"middlewares,omitempty"`
	TLS         *routerTLS `yaml:"tls,omitempty"`
}

type routerTLS struct {
	CertResolver string `yaml:"certResolver"`
}

type service struct {
	LoadBalancer loadBalancer `yaml:"loadBalancer"`
}

type loadBalancer struct {
	Servers []server `yaml:"servers"`
}

type server struct {
	URL string `yaml:"url"`
}

type middleware struct {
	RedirectScheme *redirectScheme `yaml:"redirectScheme,omitempty"`
}

type redirectScheme struct {
	Scheme    string `yaml:"scheme"`
	Permanent bool   `yaml:"permanent"`
}

// Write renders spec into <dir>/<slug>.yml atomically: write to a temp
// sibling, fsync, then rename. Traefik's file watcher tolerates the rename
// (it's a single inode swap). The directory is created if missing.
func Write(dir string, spec RouterSpec) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("traefik: ensure dir: %w", err)
	}

	name := "teal-" + spec.Slug
	rule := renderHostRule(spec.Domains)
	httpRouter := router{
		Rule:        rule,
		Service:     name,
		EntryPoints: []string{EntryPointWeb},
	}
	doc := dynamicFile{HTTP: httpSection{
		Routers: map[string]router{name: httpRouter},
		Services: map[string]service{
			name: {
				LoadBalancer: loadBalancer{
					Servers: []server{{URL: spec.BackendURL}},
				},
			},
		},
	}}

	if spec.TLSEnabled {
		secureName := name + "-secure"
		doc.HTTP.Routers[secureName] = router{
			Rule:        rule,
			Service:     name,
			EntryPoints: []string{EntryPointWebSecure},
			TLS:         &routerTLS{CertResolver: CertResolver},
		}
		if spec.HTTPSRedirect {
			redirectName := name + "-redirect"
			doc.HTTP.Middlewares = map[string]middleware{
				redirectName: {RedirectScheme: &redirectScheme{Scheme: "https", Permanent: true}},
			}
			r := doc.HTTP.Routers[name]
			r.Middlewares = []string{redirectName}
			doc.HTTP.Routers[name] = r
		}
	}

	body, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("traefik: marshal: %w", err)
	}

	target := Path(dir, spec.Slug)
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("traefik: create tmp: %w", err)
	}
	if _, err := f.Write(body); err != nil {
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

// Delete removes the dynamic-config file for an app. Returns nil when the
// file is already absent — un-routing is idempotent.
func Delete(dir, slug string) error {
	err := os.Remove(Path(dir, slug))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("traefik: delete: %w", err)
	}
	return nil
}

// Path returns the on-disk path for an app's dynamic-config file. Exposed
// so tests can assert the layout without re-implementing the convention.
func Path(dir, slug string) string {
	return filepath.Join(dir, slug+".yml")
}

func validateSpec(s RouterSpec) error {
	if s.Slug == "" {
		return errors.New("traefik: slug is required")
	}
	if len(s.Domains) == 0 {
		return errors.New("traefik: at least one domain required")
	}
	for _, d := range s.Domains {
		if strings.TrimSpace(d) == "" {
			return errors.New("traefik: domain entries must not be blank")
		}
	}
	if s.BackendURL == "" {
		return errors.New("traefik: BackendURL is required")
	}
	if _, err := url.Parse(s.BackendURL); err != nil {
		return fmt.Errorf("traefik: BackendURL invalid: %w", err)
	}
	return nil
}

// renderHostRule builds Traefik's Host matcher. One domain is wrapped in
// Host(); multiple domains are OR'd with `||`. Backticks are intentional —
// Traefik's rule grammar requires them around literals.
func renderHostRule(domains []string) string {
	parts := make([]string, 0, len(domains))
	for _, d := range domains {
		parts = append(parts, "Host(`"+strings.TrimSpace(d)+"`)")
	}
	return strings.Join(parts, " || ")
}
