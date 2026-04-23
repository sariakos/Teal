package traefik

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CertResolver is the name of the ACME resolver Teal configures. It's
// referenced from per-app dynconf when TLS is enabled. Constant so the
// dynconf and static-config writers can't drift.
const CertResolver = "letsencrypt"

// EntryPointWeb / EntryPointWebSecure are the Traefik entrypoint names.
// HTTPS is configured only when ACMEEmail is non-empty in StaticOptions.
const (
	EntryPointWeb       = "web"
	EntryPointWebSecure = "websecure"
)

// StaticOptions captures the bits of platform_settings the static Traefik
// config depends on. Empty ACMEEmail disables HTTPS entirely (admin hasn't
// configured it yet); the file is still written so Traefik has a complete
// static config and per-app dynconf can fall back to HTTP-only routers.
type StaticOptions struct {
	// ACMEEmail is the contact address Let's Encrypt requires for cert
	// registration. Empty disables the ACME resolver block.
	ACMEEmail string

	// ACMEStaging routes registrations to LE's staging CA. Use this in
	// dev/testing to avoid burning rate-limit on real certs.
	ACMEStaging bool

	// ACMEStorage is the path INSIDE the Traefik container where the
	// ACME state file lives (acme.json). Mounted via docker-compose.
	ACMEStorage string

	// DashboardInsecure publishes the unauthenticated dashboard on :8080.
	// Dev only — never set in prod.
	DashboardInsecure bool
}

// staticDoc is the YAML shape of Traefik v3's static config file. Only
// the subsections Teal cares about are modelled; missing fields fall back
// to Traefik defaults.
type staticDoc struct {
	API                  staticAPI                  `yaml:"api"`
	EntryPoints          map[string]staticEntryPoint `yaml:"entryPoints"`
	Providers            staticProviders            `yaml:"providers"`
	CertificatesResolvers map[string]staticResolver  `yaml:"certificatesResolvers,omitempty"`
	Log                  staticLog                  `yaml:"log"`
	AccessLog            *staticAccessLog           `yaml:"accessLog,omitempty"`
}

type staticAPI struct {
	Dashboard bool `yaml:"dashboard"`
	Insecure  bool `yaml:"insecure,omitempty"`
}

type staticEntryPoint struct {
	Address string `yaml:"address"`
}

type staticProviders struct {
	File staticFileProvider `yaml:"file"`
}

type staticFileProvider struct {
	Directory string `yaml:"directory"`
	Watch     bool   `yaml:"watch"`
}

type staticResolver struct {
	ACME staticACME `yaml:"acme"`
}

type staticACME struct {
	Email     string         `yaml:"email"`
	Storage   string         `yaml:"storage"`
	CAServer  string         `yaml:"caServer,omitempty"`
	HTTPChallenge staticHTTPChallenge `yaml:"httpChallenge"`
}

type staticHTTPChallenge struct {
	EntryPoint string `yaml:"entryPoint"`
}

type staticLog struct {
	Level string `yaml:"level"`
}

type staticAccessLog struct{}

// BuildStatic renders the static config bytes for the given options.
// Pure function — no I/O, suitable for tests.
func BuildStatic(opts StaticOptions) ([]byte, error) {
	doc := staticDoc{
		API: staticAPI{
			Dashboard: true,
			Insecure:  opts.DashboardInsecure,
		},
		EntryPoints: map[string]staticEntryPoint{
			EntryPointWeb: {Address: ":80"},
		},
		Providers: staticProviders{
			File: staticFileProvider{
				Directory: "/etc/traefik/dynamic",
				Watch:     true,
			},
		},
		Log: staticLog{Level: "INFO"},
	}
	if opts.ACMEEmail != "" {
		doc.EntryPoints[EntryPointWebSecure] = staticEntryPoint{Address: ":443"}
		storage := opts.ACMEStorage
		if storage == "" {
			storage = "/etc/traefik/acme/acme.json"
		}
		resolver := staticResolver{
			ACME: staticACME{
				Email:   opts.ACMEEmail,
				Storage: storage,
				HTTPChallenge: staticHTTPChallenge{
					EntryPoint: EntryPointWeb,
				},
			},
		}
		if opts.ACMEStaging {
			resolver.ACME.CAServer = "https://acme-staging-v02.api.letsencrypt.org/directory"
		}
		doc.CertificatesResolvers = map[string]staticResolver{
			CertResolver: resolver,
		}
	}
	return yaml.Marshal(doc)
}

// WriteStatic atomically writes the static config to path. Caller is
// responsible for restarting Traefik — the static config is read at boot.
func WriteStatic(path string, opts StaticOptions) error {
	body, err := BuildStatic(opts)
	if err != nil {
		return fmt.Errorf("traefik: build static: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("traefik: ensure static dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("traefik: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("traefik: rename static: %w", err)
	}
	return nil
}
