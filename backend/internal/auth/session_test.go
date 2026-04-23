package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// newTestStore opens a fresh on-disk store under t.TempDir(). Tests in this
// package need real persistence to exercise SessionManager.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "teal.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// mustUser creates a user and returns it. Uses bcrypt MinCost via direct
// call to keep test runtime down — HashPassword would be ~250ms each.
func mustUser(t *testing.T, st *store.Store) domain.User {
	t.Helper()
	u, err := st.Users.Create(context.Background(), domain.User{
		Email:        "u@x",
		PasswordHash: []byte("placeholder"),
		Role:         domain.UserRoleAdmin,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestSessionIssueValidateDestroy(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	mgr := NewSessionManager(st.Sessions, false)
	user := mustUser(t, st)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	sess, err := mgr.Issue(ctx, w, r, user.ID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if sess.ID == "" || sess.CSRFToken == "" {
		t.Fatal("Issue did not populate ID or CSRF token")
	}

	resp := w.Result()
	r2 := httptest.NewRequest("GET", "/me", nil)
	for _, c := range resp.Cookies() {
		r2.AddCookie(c)
	}
	got, err := mgr.Validate(ctx, r2)
	if err != nil {
		t.Fatalf("Validate after Issue: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("Validate returned wrong session: %+v", got)
	}

	w2 := httptest.NewRecorder()
	if err := mgr.Destroy(ctx, w2, r2); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := mgr.Validate(ctx, r2); !errors.Is(err, ErrNoSession) {
		t.Errorf("after Destroy: want ErrNoSession, got %v", err)
	}
}

func TestSessionValidateExpiredReturnsErrNoSession(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	mgr := NewSessionManager(st.Sessions, false)
	mgr.TTL = -time.Hour
	user := mustUser(t, st)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	if _, err := mgr.Issue(ctx, w, r, user.ID); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r2 := httptest.NewRequest("GET", "/me", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	if _, err := mgr.Validate(ctx, r2); !errors.Is(err, ErrNoSession) {
		t.Errorf("expired session: want ErrNoSession, got %v", err)
	}
}

func TestSessionTouchSkipsWithinSlideMinimum(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	mgr := NewSessionManager(st.Sessions, false)
	mgr.SlideMinimum = time.Hour
	user := mustUser(t, st)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	sess, _ := mgr.Issue(ctx, w, r, user.ID)

	if err := mgr.Touch(ctx, sess); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ := st.Sessions.Get(ctx, sess.ID)
	if !got.LastSeenAt.Equal(sess.LastSeenAt) {
		t.Errorf("LastSeenAt should not advance within SlideMinimum; before=%s after=%s",
			sess.LastSeenAt, got.LastSeenAt)
	}
}

func TestClientIPHonoursXForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")

	if got := clientIP(r); got != "203.0.113.5" {
		t.Errorf("clientIP = %q, want 203.0.113.5", got)
	}
}
