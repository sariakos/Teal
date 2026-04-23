package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

func mustUser(t *testing.T, st *Store) domain.User {
	t.Helper()
	u, err := st.Users.Create(context.Background(), domain.User{
		Email: "u@x", PasswordHash: []byte("h"), Role: domain.UserRoleAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSessionCreateGetTouchDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)

	expires := time.Now().Add(time.Hour).UTC()
	created, err := st.Sessions.Create(ctx, domain.Session{
		ID:        "sess-abc",
		UserID:    user.ID,
		CSRFToken: "csrf-abc",
		IP:        "127.0.0.1",
		UserAgent: "test/1.0",
		ExpiresAt: expires,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() || created.LastSeenAt.IsZero() {
		t.Error("Create did not stamp times")
	}

	got, err := st.Sessions.Get(ctx, "sess-abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != user.ID || got.CSRFToken != "csrf-abc" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	newExpires := expires.Add(time.Hour)
	newSeen := time.Now().Add(time.Minute).UTC()
	if err := st.Sessions.Touch(ctx, "sess-abc", newSeen, newExpires); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got2, _ := st.Sessions.Get(ctx, "sess-abc")
	if !got2.ExpiresAt.Equal(newExpires) {
		t.Errorf("expires_at not updated: %s vs %s", got2.ExpiresAt, newExpires)
	}

	if err := st.Sessions.Delete(ctx, "sess-abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Sessions.Get(ctx, "sess-abc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete: want ErrNotFound, got %v", err)
	}

	// Logout twice should not error.
	if err := st.Sessions.Delete(ctx, "sess-abc"); err != nil {
		t.Errorf("Delete should be idempotent, got %v", err)
	}
}

func TestSessionDeleteForUserAndExpired(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)
	now := time.Now().UTC()

	for i, exp := range []time.Time{
		now.Add(time.Hour),
		now.Add(time.Hour),
		now.Add(-time.Minute), // already expired
	} {
		_, err := st.Sessions.Create(ctx, domain.Session{
			ID: "s" + string(rune('0'+i)), UserID: user.ID, CSRFToken: "c",
			ExpiresAt: exp,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.Sessions.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteExpired returned %d, want 1", n)
	}

	if err := st.Sessions.DeleteForUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}
	if _, err := st.Sessions.Get(ctx, "s0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after DeleteForUser: want ErrNotFound, got %v", err)
	}
}

func TestAPIKeyCreateGetByHashRevoke(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)

	hash := []byte("0123456789abcdef0123456789abcdef")
	created, err := st.APIKeys.Create(ctx, domain.APIKey{
		UserID: user.ID, Name: "ci", KeyHash: hash,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("ID not assigned")
	}

	got, err := st.APIKeys.GetByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetByHash returned wrong key: %+v", got)
	}

	// MarkUsed should not change visibility.
	now := time.Now().UTC()
	if err := st.APIKeys.MarkUsed(ctx, created.ID, now); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}

	if err := st.APIKeys.Revoke(ctx, created.ID, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := st.APIKeys.GetByHash(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Revoke: GetByHash should miss, got %v", err)
	}
	// But the row still exists (audit-log link remains valid).
	if _, err := st.APIKeys.Get(ctx, created.ID); err != nil {
		t.Errorf("row should still exist after revoke: %v", err)
	}

	// Revoking again is an ErrNotFound (no rows match the WHERE).
	if err := st.APIKeys.Revoke(ctx, created.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Revoke: want ErrNotFound, got %v", err)
	}
}

func TestAPIKeyHashIsUnique(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)
	hash := []byte("samesamesamesamesamesamesamesame")

	if _, err := st.APIKeys.Create(ctx, domain.APIKey{UserID: user.ID, Name: "a", KeyHash: hash}); err != nil {
		t.Fatal(err)
	}
	_, err := st.APIKeys.Create(ctx, domain.APIKey{UserID: user.ID, Name: "b", KeyHash: hash})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate hash should yield ErrConflict, got %v", err)
	}
}
