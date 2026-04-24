package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/store"
)

// fakeDockerClient is a no-op implementation used by router tests so we
// don't depend on a running daemon.
type fakeDockerClient struct{}

func (fakeDockerClient) ListContainers(ctx context.Context) ([]docker.Container, error) {
	return nil, nil
}
func (fakeDockerClient) ListNetworks(ctx context.Context) ([]docker.Network, error) {
	return nil, nil
}
func (fakeDockerClient) ListVolumes(ctx context.Context) ([]docker.Volume, error) {
	return nil, nil
}
func (fakeDockerClient) NetworkCreateIfMissing(ctx context.Context, _ string, _ map[string]string) error {
	return nil
}
func (fakeDockerClient) ContainerInspect(ctx context.Context, _ string) (docker.ContainerInspect, error) {
	return docker.ContainerInspect{}, nil
}
func (fakeDockerClient) ContainerStats(ctx context.Context, _ string) (docker.ContainerStats, error) {
	return docker.ContainerStats{}, nil
}
func (fakeDockerClient) StreamContainerLogs(ctx context.Context, _ string) (<-chan docker.ContainerLogLine, <-chan error, error) {
	lines := make(chan docker.ContainerLogLine)
	errs := make(chan error, 1)
	close(lines)
	close(errs)
	return lines, errs, nil
}
func (fakeDockerClient) TailContainerLogs(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (fakeDockerClient) VolumeRemove(ctx context.Context, _ string, _ bool) error { return nil }
func (fakeDockerClient) Ping(ctx context.Context) error                           { return nil }
func (fakeDockerClient) Close() error                                              { return nil }

func newTestAPI(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "teal.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	authn := &auth.Authenticator{
		Sessions: auth.NewSessionManager(st.Sessions, false),
		APIKeys:  auth.NewAPIKeyManager(st.APIKeys),
		Users:    st.Users,
	}
	codec, err := crypto.NewCodec([]byte("test-secret-padding-to-32-byte-min!!"))
	if err != nil {
		t.Fatalf("crypto.NewCodec: %v", err)
	}
	deps := Deps{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:         st,
		Docker:        fakeDockerClient{},
		Authenticator: authn,
		RateLimiter:   auth.NewLoginRateLimiter(50, time.Minute),
		Codec:         codec,
	}
	return newRouter(deps), st
}

// jsonReq is a small helper to compose a JSON request.
func jsonReq(method, path string, body any) *http.Request {
	var r *http.Request
	if body != nil {
		buf, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(buf))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	return r
}

func TestBootstrapLoginMeLogoutFlow(t *testing.T) {
	h, st := newTestAPI(t)

	// Bootstrap an admin.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/register-bootstrap", map[string]string{
		"email": "admin@example.com", "password": "correct horse battery staple",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("bootstrap did not set cookies")
	}

	// /me works with the cookie.
	rec = httptest.NewRecorder()
	req := jsonReq("GET", "/api/v1/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var me meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("me decode: %v", err)
	}
	if me.User.Email != "admin@example.com" || me.User.Role != "admin" {
		t.Errorf("me payload unexpected: %+v", me.User)
	}
	if me.CSRFToken == "" {
		t.Error("me did not include CSRF token")
	}

	// Re-bootstrap should now fail.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/register-bootstrap", map[string]string{
		"email": "second@x", "password": "correct horse battery staple",
	}))
	if rec.Code != http.StatusConflict {
		t.Errorf("second bootstrap: status = %d, want 409", rec.Code)
	}

	// Logout (POST requires CSRF; provide it).
	rec = httptest.NewRecorder()
	req = jsonReq("POST", "/api/v1/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set(auth.CSRFHeaderName, me.CSRFToken)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// /me after logout — cookies still present client-side (we didn't clear
	// them in this test) but the server-side row is gone, so 401.
	rec = httptest.NewRecorder()
	req = jsonReq("GET", "/api/v1/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout: status = %d, want 401", rec.Code)
	}

	// Audit log should have at least the bootstrap and login events.
	rows, err := st.AuditLogs.List(context.Background(), 100)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if len(rows) < 1 {
		t.Errorf("audit logs: %d rows, want >= 1", len(rows))
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	h, _ := newTestAPI(t)

	// Bootstrap an admin so the user table is non-empty.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/register-bootstrap", map[string]string{
		"email": "admin@example.com", "password": "correct horse battery staple",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d", rec.Code)
	}

	// Wrong password.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/login", map[string]string{
		"email": "admin@example.com", "password": "wrong wrong wrong wrong",
	}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: status = %d, want 401", rec.Code)
	}

	// Unknown email.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/login", map[string]string{
		"email": "nobody@example.com", "password": "anything anything",
	}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown email: status = %d, want 401", rec.Code)
	}
}

func TestUnsafeMethodWithoutCSRFIsRejected(t *testing.T) {
	h, _ := newTestAPI(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/register-bootstrap", map[string]string{
		"email": "admin@example.com", "password": "correct horse battery staple",
	}))
	cookies := rec.Result().Cookies()

	// POST /apikeys without the CSRF header.
	rec = httptest.NewRecorder()
	req := jsonReq("POST", "/api/v1/apikeys", map[string]string{"name": "ci"})
	for _, c := range cookies {
		req.AddCookie(c)
	}
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF: status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
}

func TestBearerAuthBypassesCSRF(t *testing.T) {
	h, st := newTestAPI(t)

	// Bootstrap admin so we can mint an API key.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/register-bootstrap", map[string]string{
		"email": "admin@example.com", "password": "correct horse battery staple",
	}))
	cookies := rec.Result().Cookies()
	var bootstrap meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &bootstrap)

	// Create an API key (with CSRF since we're cookie-authed).
	rec = httptest.NewRecorder()
	req := jsonReq("POST", "/api/v1/apikeys", map[string]string{"name": "ci"})
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set(auth.CSRFHeaderName, bootstrap.CSRFToken)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var keyResp apiKeyCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &keyResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(keyResp.Key, auth.APIKeyPrefix) {
		t.Errorf("key shape: %q", keyResp.Key)
	}

	// Use the key from a fresh request, no cookies, no CSRF — should work.
	rec = httptest.NewRecorder()
	req = jsonReq("DELETE", "/api/v1/apikeys/"+itoa(keyResp.ID), nil)
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("revoke via bearer: status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// And the audit log records it.
	rows, _ := st.AuditLogs.List(context.Background(), 100)
	found := false
	for _, r := range rows {
		if strings.Contains(r.Details, "revoked api key") {
			found = true
		}
	}
	if !found {
		t.Error("revoke audit row not found")
	}
}

func itoa(n int64) string {
	return jsonNumber(n)
}

// jsonNumber avoids importing strconv in every helper file. fmt.Sprint is
// allocation-y but this is test code; keep it readable.
func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
