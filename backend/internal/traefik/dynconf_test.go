package traefik

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWriteAndDelete(t *testing.T) {
	dir := t.TempDir()
	spec := RouterSpec{
		Slug:       "myapp",
		Domains:    []string{"app.local", "app.example.com"},
		BackendURL: "http://172.18.0.5:80",
	}

	if err := Write(dir, spec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "myapp.yml"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var doc dynamicFile
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse back: %v", err)
	}

	r, ok := doc.HTTP.Routers["teal-myapp"]
	if !ok {
		t.Fatal("router teal-myapp missing")
	}
	if !strings.Contains(r.Rule, "Host(`app.local`)") || !strings.Contains(r.Rule, "Host(`app.example.com`)") {
		t.Errorf("rule = %q, missing one of the domains", r.Rule)
	}
	if !strings.Contains(r.Rule, "||") {
		t.Errorf("rule = %q, want OR between domains", r.Rule)
	}
	if r.EntryPoints[0] != EntryPoint {
		t.Errorf("entry point = %q, want %q", r.EntryPoints[0], EntryPoint)
	}

	s, ok := doc.HTTP.Services["teal-myapp"]
	if !ok {
		t.Fatal("service teal-myapp missing")
	}
	if s.LoadBalancer.Servers[0].URL != "http://172.18.0.5:80" {
		t.Errorf("server URL = %q", s.LoadBalancer.Servers[0].URL)
	}

	// Delete is idempotent.
	if err := Delete(dir, "myapp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := Delete(dir, "myapp"); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	// Verify the rename pattern: after a successful Write, the .tmp file
	// must NOT exist alongside the target.
	dir := t.TempDir()
	spec := RouterSpec{
		Slug: "x", Domains: []string{"x.local"}, BackendURL: "http://1.2.3.4",
	}
	if err := Write(dir, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir, "x") + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file should be cleaned up, got err=%v", err)
	}
}

func TestWriteRejectsBadSpec(t *testing.T) {
	dir := t.TempDir()
	cases := []RouterSpec{
		{Slug: "", Domains: []string{"x"}, BackendURL: "http://x"},
		{Slug: "x", Domains: nil, BackendURL: "http://x"},
		{Slug: "x", Domains: []string{"   "}, BackendURL: "http://x"},
		{Slug: "x", Domains: []string{"x"}, BackendURL: ""},
		{Slug: "x", Domains: []string{"x"}, BackendURL: "http://%zz"},
	}
	for i, c := range cases {
		if err := Write(dir, c); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestRenderHostRuleSingleDomain(t *testing.T) {
	got := renderHostRule([]string{"only.example.com"})
	want := "Host(`only.example.com`)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
