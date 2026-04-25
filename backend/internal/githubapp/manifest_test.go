package githubapp

import (
	"strings"
	"testing"
)

func TestBuildManifest_DerivesURLsFromBase(t *testing.T) {
	m := BuildManifest("https://srv.example.com", "")
	if m.Name != "Teal" {
		t.Errorf("Name = %q, want default 'Teal'", m.Name)
	}
	if m.URL != "https://srv.example.com" {
		t.Errorf("URL = %q", m.URL)
	}
	wantHook := "https://srv.example.com/api/v1/webhooks/github-app"
	if m.HookAttributes.URL != wantHook {
		t.Errorf("hook url = %q, want %q", m.HookAttributes.URL, wantHook)
	}
	if !m.HookAttributes.Active {
		t.Error("hook should be active=true; otherwise GitHub doesn't send pushes")
	}
	wantRedirect := "https://srv.example.com/api/v1/settings/github-app/manifest-callback"
	if m.RedirectURL != wantRedirect {
		t.Errorf("redirect url = %q, want %q", m.RedirectURL, wantRedirect)
	}
	if m.Public {
		t.Error("manifest should be Public=false (single-tenant App)")
	}
}

func TestBuildManifest_TrimsTrailingSlashFromBase(t *testing.T) {
	m := BuildManifest("https://srv.example.com/", "")
	if strings.Contains(m.HookAttributes.URL, "//api") {
		t.Errorf("trailing slash leaked into hook url: %q", m.HookAttributes.URL)
	}
}

func TestBuildManifest_HasMinimalPermsAndPushEvent(t *testing.T) {
	m := BuildManifest("https://srv.example.com", "Custom")
	if m.Name != "Custom" {
		t.Errorf("Name override ignored: got %q", m.Name)
	}
	if m.DefaultPerms["contents"] != "read" {
		t.Errorf("contents perm = %q, want read", m.DefaultPerms["contents"])
	}
	if m.DefaultPerms["metadata"] != "read" {
		t.Errorf("metadata perm = %q, want read", m.DefaultPerms["metadata"])
	}
	if len(m.DefaultEvents) != 1 || m.DefaultEvents[0] != "push" {
		t.Errorf("events = %v, want [push]", m.DefaultEvents)
	}
}

func TestManifestCreateURL(t *testing.T) {
	if got := ManifestCreateURL(""); got != "https://github.com/settings/apps/new" {
		t.Errorf("user-owned URL = %q", got)
	}
	if got := ManifestCreateURL("acme-inc"); got != "https://github.com/organizations/acme-inc/settings/apps/new" {
		t.Errorf("org-owned URL = %q", got)
	}
}
