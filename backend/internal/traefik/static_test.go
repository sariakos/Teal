package traefik

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildStaticHTTPOnlyByDefault(t *testing.T) {
	body, err := BuildStatic(StaticOptions{})
	if err != nil {
		t.Fatalf("BuildStatic: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "address: :80") {
		t.Errorf("missing :80 entrypoint: %s", s)
	}
	if strings.Contains(s, "websecure") {
		t.Errorf("websecure must be absent without ACMEEmail: %s", s)
	}
	if strings.Contains(s, "certificatesResolvers") {
		t.Errorf("resolver must be absent without ACMEEmail: %s", s)
	}
}

func TestBuildStaticEnablesACMEWhenEmailSet(t *testing.T) {
	body, err := BuildStatic(StaticOptions{
		ACMEEmail:   "ops@example.com",
		ACMEStaging: true,
	})
	if err != nil {
		t.Fatalf("BuildStatic: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"address: :443",
		"letsencrypt:",
		"email: ops@example.com",
		"acme-staging-v02",
		"httpChallenge:",
		"entryPoint: web",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in static config, body=\n%s", want, s)
		}
	}
}

func TestWriteStaticAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traefik.yml")
	if err := WriteStatic(path, StaticOptions{ACMEEmail: "x@y"}); err != nil {
		t.Fatalf("WriteStatic: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not present: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("tmp leak")
	}
}

type fakeSettings map[string]string

func (f fakeSettings) GetOrDefault(_ context.Context, key, def string) (string, error) {
	if v, ok := f[key]; ok {
		return v, nil
	}
	return def, nil
}

func TestApplyStaticFromSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traefik.yml")

	settings := fakeSettings{
		"acme.email":   "ops@example.com",
		"acme.staging": "true",
	}
	if err := ApplyStaticFromSettings(context.Background(), settings, path, true); err != nil {
		t.Fatalf("ApplyStaticFromSettings: %v", err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	if !strings.Contains(s, "email: ops@example.com") {
		t.Errorf("email not applied: %s", s)
	}
	if !strings.Contains(s, "insecure: true") {
		t.Errorf("dashboard insecure not applied: %s", s)
	}
}

func TestWriteEmitsTLSRoutersWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, RouterSpec{
		Slug:          "tlsapp",
		Domains:       []string{"a.example.com"},
		BackendURL:    "http://10.0.0.1:80",
		TLSEnabled:    true,
		HTTPSRedirect: true,
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "tlsapp.yml"))
	s := string(body)
	for _, want := range []string{
		"teal-tlsapp:",
		"teal-tlsapp-secure:",
		"certResolver: letsencrypt",
		"teal-tlsapp-redirect:",
		"redirectScheme:",
		"scheme: https",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in dynconf, body=\n%s", want, s)
		}
	}
}

func TestWriteHTTPOnlyWhenTLSDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, RouterSpec{
		Slug:       "noTLS",
		Domains:    []string{"a.example.com"},
		BackendURL: "http://10.0.0.1:80",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "noTLS.yml"))
	s := string(body)
	if strings.Contains(s, "-secure") {
		t.Errorf("must not emit secure router when TLS disabled: %s", s)
	}
	if strings.Contains(s, "redirectScheme") {
		t.Errorf("must not emit redirect when TLS disabled: %s", s)
	}
}
