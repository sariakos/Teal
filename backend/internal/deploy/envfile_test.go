package deploy

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// fakeCodec returns ciphertext as the plaintext (no actual crypto). Lets
// us test hydration shape independently of key material.
type fakeCodec struct{}

func (fakeCodec) Open(_, _ string, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

func newHydrationStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestHydrateEnvEmpty(t *testing.T) {
	ctx := context.Background()
	st := newHydrationStore(t)
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})

	res, err := hydrateEnv(ctx, st, fakeCodec{}, app)
	if err != nil {
		t.Fatalf("hydrate empty: %v", err)
	}
	if res.Hash != "" || len(res.Body) != 0 {
		t.Errorf("empty: got body=%q hash=%q", res.Body, res.Hash)
	}
}

func TestHydrateEnvAppAndSharedSorted(t *testing.T) {
	ctx := context.Background()
	st := newHydrationStore(t)
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})

	// Two app vars, inserted in non-sorted order.
	if _, err := st.EnvVars.Upsert(ctx, app.ID, "Z_LAST", []byte("zzz")); err != nil {
		t.Fatalf("upsert Z: %v", err)
	}
	if _, err := st.EnvVars.Upsert(ctx, app.ID, "A_FIRST", []byte("aaa")); err != nil {
		t.Fatalf("upsert A: %v", err)
	}

	// One shared var, opted in.
	if _, err := st.EnvVars.UpsertShared(ctx, "M_MID", []byte("mmm")); err != nil {
		t.Fatalf("upsert shared: %v", err)
	}
	if err := st.AppSharedEnvVars.Set(ctx, app.ID, []string{"M_MID"}); err != nil {
		t.Fatalf("set allow-list: %v", err)
	}

	res, err := hydrateEnv(ctx, st, fakeCodec{}, app)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	want := "A_FIRST=aaa\nM_MID=mmm\nZ_LAST=zzz\n"
	if string(res.Body) != want {
		t.Errorf("body = %q, want %q", res.Body, want)
	}
	if res.Hash == "" {
		t.Error("hash empty for non-empty body")
	}

	// Stable hash regardless of insertion order: re-hydrate after re-upsert
	// in a different order and compare.
	if _, err := st.EnvVars.Upsert(ctx, app.ID, "Z_LAST", []byte("zzz")); err != nil {
		t.Fatalf("re-upsert Z: %v", err)
	}
	res2, _ := hydrateEnv(ctx, st, fakeCodec{}, app)
	if res2.Hash != res.Hash {
		t.Errorf("hash unstable: %q vs %q", res.Hash, res2.Hash)
	}
}

func TestHydrateEnvAppShadowsShared(t *testing.T) {
	ctx := context.Background()
	st := newHydrationStore(t)
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})

	if _, err := st.EnvVars.Upsert(ctx, app.ID, "DUP", []byte("from-app")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnvVars.UpsertShared(ctx, "DUP", []byte("from-shared")); err != nil {
		t.Fatal(err)
	}
	if err := st.AppSharedEnvVars.Set(ctx, app.ID, []string{"DUP"}); err != nil {
		t.Fatal(err)
	}

	res, err := hydrateEnv(ctx, st, fakeCodec{}, app)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if string(res.Body) != "DUP=from-app\n" {
		t.Errorf("expected app to shadow shared: %q", res.Body)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "shadowed by per-app") {
		t.Errorf("expected shadow warning: %v", res.Warnings)
	}
}

func TestHydrateEnvOptedInButMissingShared(t *testing.T) {
	ctx := context.Background()
	st := newHydrationStore(t)
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})

	if err := st.AppSharedEnvVars.Set(ctx, app.ID, []string{"GHOST"}); err != nil {
		t.Fatal(err)
	}

	res, err := hydrateEnv(ctx, st, fakeCodec{}, app)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if len(res.Body) != 0 {
		t.Errorf("missing shared should not contribute: %q", res.Body)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "no shared row") {
		t.Errorf("expected missing-shared warning: %v", res.Warnings)
	}
}
