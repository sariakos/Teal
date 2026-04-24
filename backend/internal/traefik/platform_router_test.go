package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePlatformRouter_HTTPOnlyBeforeACME(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlatformRouter(dir, PlatformRouterOptions{
		BaseDomain: "srv.example.com",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readFile(t, filepath.Join(dir, "_platform.yml"))
	if !strings.Contains(s, "teal-platform:") {
		t.Error("HTTP router missing")
	}
	if strings.Contains(s, "teal-platform-secure") {
		t.Error("should not emit secure router without TLS")
	}
	if strings.Contains(s, "redirectScheme") {
		t.Error("should not redirect without TLS")
	}
}

func TestWritePlatformRouter_HTTPSRedirectWhenTLSOn(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlatformRouter(dir, PlatformRouterOptions{
		BaseDomain:    "srv.example.com",
		TLSEnabled:    true,
		HTTPSRedirect: true,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readFile(t, filepath.Join(dir, "_platform.yml"))
	if !strings.Contains(s, "teal-platform-secure") {
		t.Error("secure router missing")
	}
	if !strings.Contains(s, "redirectScheme") {
		t.Error("redirect middleware missing when HTTPSRedirect=true")
	}
	if !strings.Contains(s, "certResolver: letsencrypt") {
		t.Error("cert resolver missing")
	}
}

func TestWritePlatformRouter_NoRedirectWhenTLSOnButRedirectOff(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlatformRouter(dir, PlatformRouterOptions{
		BaseDomain:    "srv.example.com",
		TLSEnabled:    true,
		HTTPSRedirect: false,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readFile(t, filepath.Join(dir, "_platform.yml"))
	if strings.Contains(s, "redirectScheme") {
		t.Error("redirect should be absent when HTTPSRedirect=false")
	}
	if !strings.Contains(s, "teal-platform-secure") {
		t.Error("secure router should still be present when TLS is on")
	}
}

func TestWritePlatformRouter_RejectsEmptyBaseDomain(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlatformRouter(dir, PlatformRouterOptions{}); err == nil {
		t.Error("expected error for empty BaseDomain")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}
