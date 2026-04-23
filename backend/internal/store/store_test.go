package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
)

// newTestStore opens a fresh on-disk SQLite under t.TempDir(). On-disk
// (rather than in-memory) so multi-connection scenarios behave as they will
// in production.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(context.Background(), filepath.Join(dir, "teal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestAppRepoCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	created, err := st.Apps.Create(ctx, domain.App{
		Slug:              "myapp",
		Name:              "My App",
		AutoDeployBranch:  "main",
		AutoDeployEnabled: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected ID to be assigned")
	}
	if created.Status != domain.AppStatusIdle {
		t.Errorf("default Status = %q, want idle", created.Status)
	}

	got, err := st.Apps.GetBySlug(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.Name != "My App" {
		t.Errorf("Name round-trip failed: %q", got.Name)
	}
	if !got.AutoDeployEnabled {
		t.Error("AutoDeployEnabled should be true")
	}

	got.Name = "Renamed"
	got.Status = domain.AppStatusRunning
	if err := st.Apps.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got2, _ := st.Apps.Get(ctx, got.ID)
	if got2.Name != "Renamed" || got2.Status != domain.AppStatusRunning {
		t.Errorf("Update did not persist: %+v", got2)
	}

	if err := st.Apps.Delete(ctx, got.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Apps.Get(ctx, got.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete: want ErrNotFound, got %v", err)
	}
}

func TestUserRepoCRUDAndSecretsHidden(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	created, err := st.Users.Create(ctx, domain.User{
		Email:        "alice@example.com",
		PasswordHash: []byte("$2y$12$fakehash"),
		Role:         domain.UserRoleAdmin,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := st.Users.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != created.ID || got.Role != domain.UserRoleAdmin {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if string(got.PasswordHash) != "$2y$12$fakehash" {
		t.Errorf("password hash not persisted: %q", got.PasswordHash)
	}
}

func TestDeploymentRepoCreateUpdateList(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	app, err := st.Apps.Create(ctx, domain.App{Slug: "x", Name: "X"})
	if err != nil {
		t.Fatal(err)
	}

	d1, err := st.Deployments.Create(ctx, domain.Deployment{
		AppID:     app.ID,
		Color:     domain.ColorBlue,
		CommitSHA: "deadbeef",
	})
	if err != nil {
		t.Fatalf("Create d1: %v", err)
	}
	if d1.Status != domain.DeploymentStatusPending {
		t.Errorf("default status = %q", d1.Status)
	}

	_, err = st.Deployments.Create(ctx, domain.Deployment{
		AppID: app.ID,
		Color: domain.ColorGreen,
	})
	if err != nil {
		t.Fatalf("Create d2: %v", err)
	}

	d1.Status = domain.DeploymentStatusSucceeded
	if err := st.Deployments.Update(ctx, d1); err != nil {
		t.Fatalf("Update: %v", err)
	}

	list, err := st.Deployments.ListForApp(ctx, app.ID, 10)
	if err != nil {
		t.Fatalf("ListForApp: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListForApp returned %d, want 2", len(list))
	}
}

func TestEnvVarScopeUniqueness(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	app, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})

	_, err := st.EnvVars.Create(ctx, domain.EnvVar{
		Scope: domain.EnvVarScopeApp, AppID: &app.ID, Key: "DB_URL", ValueEncrypted: []byte("c1"),
	})
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = st.EnvVars.Create(ctx, domain.EnvVar{
		Scope: domain.EnvVarScopeApp, AppID: &app.ID, Key: "DB_URL", ValueEncrypted: []byte("c2"),
	})
	if err == nil {
		t.Error("expected uniqueness violation for (app, key)")
	}

	_, err = st.EnvVars.Create(ctx, domain.EnvVar{
		Scope: domain.EnvVarScopeShared, Key: "GLOBAL", ValueEncrypted: []byte("c"),
	})
	if err != nil {
		t.Fatalf("shared insert: %v", err)
	}
}

func TestEnvVarScopeAppIDConsistency(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// Shared with non-nil AppID must fail.
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "z", Name: "Z"})
	_, err := st.EnvVars.Create(ctx, domain.EnvVar{
		Scope: domain.EnvVarScopeShared, AppID: &app.ID, Key: "X", ValueEncrypted: []byte("c"),
	})
	if err == nil {
		t.Error("expected CHECK violation for shared+app_id")
	}

	// App with nil AppID must fail.
	_, err = st.EnvVars.Create(ctx, domain.EnvVar{
		Scope: domain.EnvVarScopeApp, AppID: nil, Key: "Y", ValueEncrypted: []byte("c"),
	})
	if err == nil {
		t.Error("expected CHECK violation for app+nil-app_id")
	}
}

func TestAuditLogAppendOnly(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	user, _ := st.Users.Create(ctx, domain.User{
		Email: "x@x", PasswordHash: []byte("h"), Role: domain.UserRoleAdmin,
	})

	for i := 0; i < 3; i++ {
		_, err := st.AuditLogs.Append(ctx, domain.AuditLog{
			ActorUserID: &user.ID, Actor: "x@x", Action: domain.AuditActionUserLogin,
			IP: "127.0.0.1",
		})
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	rows, err := st.AuditLogs.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("List returned %d rows, want 3", len(rows))
	}
	for _, r := range rows {
		if r.Action != domain.AuditActionUserLogin {
			t.Errorf("unexpected action %q", r.Action)
		}
	}
}

func TestAppActiveColorAndStatusUpdates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	app, err := st.Apps.Create(ctx, domain.App{
		Slug: "x", Name: "X", Domains: "x.local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ActiveColor != "" {
		t.Errorf("ActiveColor on create = %q, want empty", app.ActiveColor)
	}

	if err := st.Apps.SetActiveColor(ctx, app.ID, domain.ColorBlue); err != nil {
		t.Fatalf("SetActiveColor: %v", err)
	}
	got, _ := st.Apps.Get(ctx, app.ID)
	if got.ActiveColor != domain.ColorBlue {
		t.Errorf("ActiveColor = %q, want blue", got.ActiveColor)
	}

	if err := st.Apps.SetStatus(ctx, app.ID, domain.AppStatusRunning); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, _ = st.Apps.Get(ctx, app.ID)
	if got.Status != domain.AppStatusRunning {
		t.Errorf("Status = %q", got.Status)
	}

	if err := st.Apps.SetStatus(ctx, 9999, domain.AppStatusFailed); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetStatus missing: want ErrNotFound, got %v", err)
	}
}

func TestErrConflictOnDuplicate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if _, err := st.Apps.Create(ctx, domain.App{Slug: "dup", Name: "first"}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := st.Apps.Create(ctx, domain.App{Slug: "dup", Name: "second"})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("Apps.Create dup: want ErrConflict, got %v", err)
	}

	if _, err := st.Users.Create(ctx, domain.User{Email: "dup@x", PasswordHash: []byte("h")}); err != nil {
		t.Fatalf("first user: %v", err)
	}
	_, err = st.Users.Create(ctx, domain.User{Email: "dup@x", PasswordHash: []byte("h")})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("Users.Create dup: want ErrConflict, got %v", err)
	}
}

func TestErrNotFound(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.Apps.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Apps.Get missing: %v", err)
	}
	if _, err := st.Users.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Users.Get missing: %v", err)
	}
	if _, err := st.Deployments.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Deployments.Get missing: %v", err)
	}
}
