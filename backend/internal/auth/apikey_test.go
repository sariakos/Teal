package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIKeyGenerateAndValidate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)
	mgr := NewAPIKeyManager(st.APIKeys)

	raw, key, err := mgr.Generate(ctx, user.ID, "ci")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(raw, APIKeyPrefix) || len(raw) < 40 {
		t.Errorf("raw key shape unexpected: %q (len %d)", raw, len(raw))
	}
	if key.UserID != user.ID || len(key.KeyHash) != 32 {
		t.Errorf("persisted key looks wrong: %+v", key)
	}

	got, err := mgr.Validate(ctx, raw)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.ID != key.ID {
		t.Errorf("Validate returned wrong key: %+v", got)
	}
}

func TestAPIKeyValidateRejectsTampered(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)
	mgr := NewAPIKeyManager(st.APIKeys)
	raw, _, _ := mgr.Generate(ctx, user.ID, "ci")

	// Flip the last byte to a different valid base32 character.
	bad := []byte(raw)
	last := bad[len(bad)-1]
	bad[len(bad)-1] = 'A'
	if last == 'A' {
		bad[len(bad)-1] = 'B'
	}
	if _, err := mgr.Validate(ctx, string(bad)); !errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("tampered: want ErrInvalidAPIKey, got %v", err)
	}
}

func TestAPIKeyValidateRejectsRevoked(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user := mustUser(t, st)
	mgr := NewAPIKeyManager(st.APIKeys)
	raw, key, _ := mgr.Generate(ctx, user.ID, "ci")

	if err := st.APIKeys.Revoke(ctx, key.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := mgr.Validate(ctx, raw); !errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("revoked: want ErrInvalidAPIKey, got %v", err)
	}
}

func TestAPIKeyValidateRejectsBadPrefix(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	mgr := NewAPIKeyManager(st.APIKeys)
	if _, err := mgr.Validate(ctx, "definitely-not-an-api-key"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("bad prefix: want ErrInvalidAPIKey, got %v", err)
	}
}

func TestParseBearer(t *testing.T) {
	cases := []struct {
		header string
		want   string
		ok     bool
	}{
		{"Bearer tk_abc", "tk_abc", true},
		{"Bearer   tk_abc  ", "tk_abc", true},
		{"bearer tk_abc", "", false}, // case-sensitive scheme is fine; we control both ends
		{"", "", false},
		{"Bearer ", "", false},
		{"Basic tk_abc", "", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}
		got, ok := ParseBearer(r)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseBearer(%q) = (%q, %v), want (%q, %v)", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}

