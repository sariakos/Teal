package store

import (
	"context"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
)

// TestAppsAcceptsGitHubAppAuthKind proves migration 0009 actually
// relaxed the CHECK constraint that 0001 baked in. Without 0009,
// inserting an app with GitAuthGitHubApp returns a CHECK constraint
// violation and the user can't create GitHub-App-backed apps.
func TestAppsAcceptsGitHubAppAuthKind(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	got, err := st.Apps.Create(ctx, domain.App{
		Slug:        "ghapp-test",
		Name:        "GH App Test",
		GitURL:      "https://github.com/example/repo.git",
		GitAuthKind: domain.GitAuthGitHubApp,
	})
	if err != nil {
		t.Fatalf("Apps.Create with GitAuthGitHubApp: %v", err)
	}
	if got.GitAuthKind != domain.GitAuthGitHubApp {
		t.Errorf("auth kind round-trip: got %q, want %q", got.GitAuthKind, domain.GitAuthGitHubApp)
	}

	// And SSH/PAT/empty should still work — the relaxed CHECK adds a
	// value, doesn't remove any.
	for _, kind := range []domain.GitAuthKind{
		domain.GitAuthNone, domain.GitAuthSSH, domain.GitAuthPAT,
	} {
		_, err := st.Apps.Create(ctx, domain.App{
			Slug:        "kind-" + string(kind) + "-x",
			Name:        "x",
			GitAuthKind: kind,
		})
		if err != nil {
			t.Errorf("Apps.Create with kind %q: %v", kind, err)
		}
	}
}
