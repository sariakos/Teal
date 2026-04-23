package store

import (
	"context"
	"errors"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
)

func TestPlatformSettingsUpsert(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if _, err := st.PlatformSettings.Get(ctx, "acme.email"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: want ErrNotFound, got %v", err)
	}
	v, err := st.PlatformSettings.GetOrDefault(ctx, "acme.email", "(unset)")
	if err != nil || v != "(unset)" {
		t.Fatalf("GetOrDefault missing: %q, %v", v, err)
	}

	if err := st.PlatformSettings.Set(ctx, "acme.email", "ops@example.com"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	s, err := st.PlatformSettings.Get(ctx, "acme.email")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.Value != "ops@example.com" {
		t.Errorf("value round-trip: %q", s.Value)
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be stamped")
	}

	// Update (conflict → overwrite)
	if err := st.PlatformSettings.Set(ctx, "acme.email", "new@example.com"); err != nil {
		t.Fatalf("update Set: %v", err)
	}
	s2, _ := st.PlatformSettings.Get(ctx, "acme.email")
	if s2.Value != "new@example.com" {
		t.Errorf("update value: %q", s2.Value)
	}

	if err := st.PlatformSettings.Set(ctx, "https.redirect_enabled", "true"); err != nil {
		t.Fatalf("Set second: %v", err)
	}
	list, err := st.PlatformSettings.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len: got %d", len(list))
	}
	// List ordered by key → acme.email, https.redirect_enabled
	if list[0].Key != "acme.email" || list[1].Key != "https.redirect_enabled" {
		t.Errorf("List order: %+v", list)
	}

	if err := st.PlatformSettings.Delete(ctx, "acme.email"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := st.PlatformSettings.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete missing key should be idempotent: %v", err)
	}
}

func TestAppSharedEnvVarsSet(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	app, err := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	// Empty → empty
	keys, err := st.AppSharedEnvVars.ListForApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListForApp empty: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("empty allow-list: got %v", keys)
	}

	// Set with duplicates + empty — should normalize.
	if err := st.AppSharedEnvVars.Set(ctx, app.ID, []string{"B", "A", "", "B"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	keys, _ = st.AppSharedEnvVars.ListForApp(ctx, app.ID)
	if len(keys) != 2 || keys[0] != "A" || keys[1] != "B" {
		t.Fatalf("Set dedup/sort: got %v", keys)
	}

	// Overwrite — removes old, adds new.
	if err := st.AppSharedEnvVars.Set(ctx, app.ID, []string{"C"}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	keys, _ = st.AppSharedEnvVars.ListForApp(ctx, app.ID)
	if len(keys) != 1 || keys[0] != "C" {
		t.Fatalf("overwrite result: %v", keys)
	}

	// Cascade: deleting the app clears the allow-list.
	if err := st.Apps.Delete(ctx, app.ID); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	keys, _ = st.AppSharedEnvVars.ListForApp(ctx, app.ID)
	if len(keys) != 0 {
		t.Errorf("after app delete: allow-list not cascaded: %v", keys)
	}
}

func TestEnvVarUpsertAndDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	app, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})

	// Upsert app-scoped.
	e1, err := st.EnvVars.Upsert(ctx, app.ID, "DATABASE_URL", []byte("cipher-1"))
	if err != nil {
		t.Fatalf("Upsert insert: %v", err)
	}
	if e1.ID == 0 || e1.Scope != domain.EnvVarScopeApp || string(e1.ValueEncrypted) != "cipher-1" {
		t.Errorf("Upsert insert state: %+v", e1)
	}

	// Re-upsert → same ID, new ciphertext.
	e2, err := st.EnvVars.Upsert(ctx, app.ID, "DATABASE_URL", []byte("cipher-2"))
	if err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	if e2.ID != e1.ID {
		t.Errorf("Upsert ID should be stable: %d vs %d", e1.ID, e2.ID)
	}
	if string(e2.ValueEncrypted) != "cipher-2" {
		t.Errorf("Upsert did not update ciphertext: %q", e2.ValueEncrypted)
	}

	// GetByAppAndKey
	got, err := st.EnvVars.GetByAppAndKey(ctx, app.ID, "DATABASE_URL")
	if err != nil {
		t.Fatalf("GetByAppAndKey: %v", err)
	}
	if got.ID != e2.ID {
		t.Errorf("GetByAppAndKey mismatch")
	}
	if _, err := st.EnvVars.GetByAppAndKey(ctx, app.ID, "NOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key: want ErrNotFound, got %v", err)
	}

	// Shared upsert + shared uniqueness is global.
	if _, err := st.EnvVars.UpsertShared(ctx, "GLOBAL", []byte("g-1")); err != nil {
		t.Fatalf("UpsertShared: %v", err)
	}
	g, err := st.EnvVars.GetShared(ctx, "GLOBAL")
	if err != nil || string(g.ValueEncrypted) != "g-1" {
		t.Fatalf("GetShared: %v %q", err, g.ValueEncrypted)
	}

	// Delete by natural key.
	if err := st.EnvVars.DeleteByAppAndKey(ctx, app.ID, "DATABASE_URL"); err != nil {
		t.Fatalf("DeleteByAppAndKey: %v", err)
	}
	if err := st.EnvVars.DeleteByAppAndKey(ctx, app.ID, "DATABASE_URL"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: want ErrNotFound, got %v", err)
	}
	if err := st.EnvVars.DeleteShared(ctx, "GLOBAL"); err != nil {
		t.Fatalf("DeleteShared: %v", err)
	}
	if err := st.EnvVars.DeleteShared(ctx, "GLOBAL"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete shared: want ErrNotFound, got %v", err)
	}
}
