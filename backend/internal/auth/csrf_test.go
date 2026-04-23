package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
)

func newCSRFTestHandler() http.Handler {
	return CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestCSRFAllowsSafeMethods(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		req := httptest.NewRequest(method, "/x", nil)
		// No session, no header — must still pass.
		rec := httptest.NewRecorder()
		newCSRFTestHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s without session: status = %d, want 200", method, rec.Code)
		}
	}
}

func TestCSRFRejectsUnsafeWithoutToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req = req.WithContext(WithSession(req.Context(), domain.Session{
		ID: "s", CSRFToken: "secret-csrf",
	}))
	rec := httptest.NewRecorder()
	newCSRFTestHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without header: status = %d, want 403", rec.Code)
	}
}

func TestCSRFRejectsUnsafeWithWrongToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set(CSRFHeaderName, "WRONG")
	req = req.WithContext(WithSession(req.Context(), domain.Session{
		ID: "s", CSRFToken: "secret-csrf",
	}))
	rec := httptest.NewRecorder()
	newCSRFTestHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with wrong header: status = %d, want 403", rec.Code)
	}
}

func TestCSRFAcceptsUnsafeWithMatchingToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set(CSRFHeaderName, "secret-csrf")
	req = req.WithContext(WithSession(req.Context(), domain.Session{
		ID: "s", CSRFToken: "secret-csrf",
	}))
	rec := httptest.NewRecorder()
	newCSRFTestHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("POST with matching header: status = %d, want 200", rec.Code)
	}
}

func TestCSRFAllowsUnsafeWithoutSession(t *testing.T) {
	// e.g. bearer-authed request — no session in ctx, CSRF not enforced.
	req := httptest.NewRequest("POST", "/x", nil)
	rec := httptest.NewRecorder()
	newCSRFTestHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("POST without session: status = %d, want 200", rec.Code)
	}
}
