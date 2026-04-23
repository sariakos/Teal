package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
)

func newTestAuthenticator(t *testing.T) (*Authenticator, *domain.User) {
	t.Helper()
	st := newTestStore(t)
	user := mustUser(t, st)
	a := &Authenticator{
		Sessions: NewSessionManager(st.Sessions, false),
		APIKeys:  NewAPIKeyManager(st.APIKeys),
		Users:    st.Users,
	}
	return a, &user
}

func TestMiddleware401WithoutCredentials(t *testing.T) {
	a, _ := newTestAuthenticator(t)
	rec := httptest.NewRecorder()
	a.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("downstream invoked without credentials")
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddlewareAcceptsCookieSession(t *testing.T) {
	a, user := newTestAuthenticator(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	if _, err := a.Sessions.Issue(context.Background(), w, r, user.ID); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r2 := httptest.NewRequest("GET", "/protected", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}

	var got Subject
	rec := httptest.NewRecorder()
	a.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, r2)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.UserID != user.ID || got.Email != user.Email {
		t.Errorf("Subject = %+v, want UserID=%d Email=%s", got, user.ID, user.Email)
	}
}

func TestMiddlewareAcceptsBearerAPIKey(t *testing.T) {
	a, user := newTestAuthenticator(t)
	raw, _, err := a.APIKeys.Generate(context.Background(), user.ID, "ci")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	r := httptest.NewRequest("GET", "/protected", nil)
	r.Header.Set("Authorization", "Bearer "+raw)

	var got Subject
	rec := httptest.NewRecorder()
	a.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got.UserID != user.ID {
		t.Errorf("Subject.UserID = %d, want %d", got.UserID, user.ID)
	}
	if SessionFromContext(r.Context()).ID != "" {
		t.Error("bearer-authed request should NOT have a session in context")
	}
}

func TestMiddlewareDevBypassAttachesAdmin(t *testing.T) {
	a, _ := newTestAuthenticator(t)
	a.DevBypass = true

	var got Subject
	rec := httptest.NewRecorder()
	a.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	if got.Role != domain.UserRoleAdmin {
		t.Errorf("Subject.Role = %q, want admin", got.Role)
	}
}

func TestRequireRole(t *testing.T) {
	cases := []struct {
		have domain.UserRole
		min  domain.UserRole
		want int
	}{
		{domain.UserRoleAdmin, domain.UserRoleViewer, http.StatusOK},
		{domain.UserRoleMember, domain.UserRoleAdmin, http.StatusForbidden},
		{domain.UserRoleViewer, domain.UserRoleMember, http.StatusForbidden},
		{"", domain.UserRoleViewer, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(string(tc.have)+"_vs_"+string(tc.min), func(t *testing.T) {
			h := RequireRole(tc.min)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest("GET", "/x", nil)
			req = req.WithContext(WithSubject(req.Context(), Subject{Role: tc.have}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
