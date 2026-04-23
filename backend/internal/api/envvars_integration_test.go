package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/domain"
)

// envSession bootstraps an admin and returns the cookies + CSRF token.
// Reused across the env-var integration tests.
func envSession(t *testing.T, h http.Handler) (cookies []*http.Cookie, csrf string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/v1/register-bootstrap", map[string]string{
		"email": "admin@example.com", "password": "correct horse battery staple",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	cookies = rec.Result().Cookies()

	rec = httptest.NewRecorder()
	req := jsonReq("GET", "/api/v1/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	h.ServeHTTP(rec, req)
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	csrf = me.CSRFToken
	return
}

func envDo(t *testing.T, h http.Handler, method, path string, body any, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	req := jsonReq(method, path, body)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if method != http.MethodGet {
		req.Header.Set(auth.CSRFHeaderName, csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAppEnvVarsCRUDAndRevealAudit(t *testing.T) {
	h, st := newTestAPI(t)
	cookies, csrf := envSession(t, h)

	// Create app.
	rec := envDo(t, h, "POST", "/api/v1/apps", map[string]any{
		"slug": "envapp", "name": "Env App",
	}, cookies, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app: %d %s", rec.Code, rec.Body.String())
	}

	// List → empty.
	rec = envDo(t, h, "GET", "/api/v1/apps/envapp/envvars", nil, cookies, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("list empty: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Errorf("list empty body = %q", rec.Body.String())
	}

	// Bad key.
	rec = envDo(t, h, "POST", "/api/v1/apps/envapp/envvars", map[string]any{
		"key": "1BAD", "value": "x",
	}, cookies, csrf)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad key: %d %s", rec.Code, rec.Body.String())
	}

	// Insert.
	rec = envDo(t, h, "POST", "/api/v1/apps/envapp/envvars", map[string]any{
		"key": "DATABASE_URL", "value": "postgres://x",
	}, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upsert: %d %s", rec.Code, rec.Body.String())
	}

	// Masked list.
	rec = envDo(t, h, "GET", "/api/v1/apps/envapp/envvars", nil, cookies, csrf)
	var rows []envVarResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Key != "DATABASE_URL" || !rows[0].Masked || rows[0].Value != "" {
		t.Errorf("masked list: %+v", rows)
	}
	if !rows[0].HasValue {
		t.Error("HasValue should be true after upsert")
	}

	// Audit log: revealing should land an envvar.reveal row.
	beforeAudits, _ := st.AuditLogs.List(context.Background(), 100)
	rec = envDo(t, h, "GET", "/api/v1/apps/envapp/envvars?reveal=true", nil, cookies, csrf)
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Value != "postgres://x" || rows[0].Masked {
		t.Errorf("reveal: %+v", rows)
	}
	afterAudits, _ := st.AuditLogs.List(context.Background(), 100)
	if len(afterAudits)-len(beforeAudits) != 1 {
		t.Errorf("reveal should add 1 audit row; got %d", len(afterAudits)-len(beforeAudits))
	}
	if afterAudits[0].Action != domain.AuditActionEnvVarReveal {
		t.Errorf("reveal audit action: %q", afterAudits[0].Action)
	}

	// Re-upsert (ciphertext changes).
	rec = envDo(t, h, "POST", "/api/v1/apps/envapp/envvars", map[string]any{
		"key": "DATABASE_URL", "value": "postgres://y",
	}, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("re-upsert: %d", rec.Code)
	}
	rec = envDo(t, h, "GET", "/api/v1/apps/envapp/envvars?reveal=true", nil, cookies, csrf)
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if rows[0].Value != "postgres://y" {
		t.Errorf("re-upsert value: %q", rows[0].Value)
	}

	// Delete.
	rec = envDo(t, h, "DELETE", "/api/v1/apps/envapp/envvars/DATABASE_URL", nil, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	// Second delete → 404.
	rec = envDo(t, h, "DELETE", "/api/v1/apps/envapp/envvars/DATABASE_URL", nil, cookies, csrf)
	if rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestSharedEnvVarAdminAndAppAllowList(t *testing.T) {
	h, _ := newTestAPI(t)
	cookies, csrf := envSession(t, h)

	rec := envDo(t, h, "POST", "/api/v1/apps", map[string]any{
		"slug": "shapp", "name": "Shared App",
	}, cookies, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app: %d %s", rec.Code, rec.Body.String())
	}

	// Admin: add a shared key.
	rec = envDo(t, h, "POST", "/api/v1/shared-envvars", map[string]any{
		"key": "SENTRY_DSN", "value": "https://shared",
	}, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upsert shared: %d %s", rec.Code, rec.Body.String())
	}

	// Per-app GET returns Available + Included.
	rec = envDo(t, h, "GET", "/api/v1/apps/shapp/shared-envvars", nil, cookies, csrf)
	var listing appSharedListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &listing)
	if len(listing.Available) != 1 || listing.Available[0] != "SENTRY_DSN" {
		t.Errorf("available: %+v", listing)
	}
	if len(listing.Included) != 0 {
		t.Errorf("included pre-set: %+v", listing.Included)
	}

	// Set allow-list.
	rec = envDo(t, h, "PUT", "/api/v1/apps/shapp/shared-envvars", map[string]any{
		"keys": []string{"SENTRY_DSN"},
	}, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set allow-list: %d %s", rec.Code, rec.Body.String())
	}
	rec = envDo(t, h, "GET", "/api/v1/apps/shapp/shared-envvars", nil, cookies, csrf)
	_ = json.Unmarshal(rec.Body.Bytes(), &listing)
	if len(listing.Included) != 1 || listing.Included[0] != "SENTRY_DSN" {
		t.Errorf("included post-set: %+v", listing.Included)
	}

	// Bad key in allow-list → 400.
	rec = envDo(t, h, "PUT", "/api/v1/apps/shapp/shared-envvars", map[string]any{
		"keys": []string{"1BAD"},
	}, cookies, csrf)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad key: %d", rec.Code)
	}

	// Reveal shared (admin).
	rec = envDo(t, h, "GET", "/api/v1/shared-envvars?reveal=true", nil, cookies, csrf)
	var rows []envVarResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Value != "https://shared" {
		t.Errorf("reveal shared: %+v", rows)
	}

	// Delete shared.
	rec = envDo(t, h, "DELETE", "/api/v1/shared-envvars/SENTRY_DSN", nil, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete shared: %d %s", rec.Code, rec.Body.String())
	}

	// App allow-list still references SENTRY_DSN — that's intentional.
	rec = envDo(t, h, "GET", "/api/v1/apps/shapp/shared-envvars", nil, cookies, csrf)
	_ = json.Unmarshal(rec.Body.Bytes(), &listing)
	if len(listing.Available) != 0 {
		t.Errorf("available after delete: %+v", listing.Available)
	}
	if len(listing.Included) != 1 {
		t.Errorf("included survives shared delete: %+v", listing.Included)
	}
}

func TestPlatformSettingsAdminCRUD(t *testing.T) {
	h, _ := newTestAPI(t)
	cookies, csrf := envSession(t, h)

	// List → empty.
	rec := envDo(t, h, "GET", "/api/v1/settings", nil, cookies, csrf)
	if rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("list empty: %d %q", rec.Code, rec.Body.String())
	}

	// Set acme.email — affects static config, response should hint restart.
	rec = envDo(t, h, "PUT", "/api/v1/settings/acme.email", map[string]any{
		"value": "ops@example.com",
	}, cookies, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	var mut settingMutationResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &mut)
	if !mut.RestartTraefik {
		t.Error("setting acme.email should hint restartTraefik=true")
	}

	// Set https.redirect_enabled — does NOT affect static config.
	rec = envDo(t, h, "PUT", "/api/v1/settings/https.redirect_enabled", map[string]any{
		"value": "true",
	}, cookies, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("set redirect: %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &mut)
	if mut.RestartTraefik {
		t.Error("redirect toggle should NOT hint restart")
	}

	// Unknown key rejected.
	rec = envDo(t, h, "PUT", "/api/v1/settings/foo.bar", map[string]any{"value": "x"}, cookies, csrf)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown key: %d", rec.Code)
	}

	// List has both keys.
	rec = envDo(t, h, "GET", "/api/v1/settings", nil, cookies, csrf)
	var list []settingResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Errorf("list len: %d (%+v)", len(list), list)
	}

	// Delete is idempotent.
	rec = envDo(t, h, "DELETE", "/api/v1/settings/acme.email", nil, cookies, csrf)
	if rec.Code != http.StatusOK {
		t.Errorf("delete: %d", rec.Code)
	}
	rec = envDo(t, h, "DELETE", "/api/v1/settings/acme.email", nil, cookies, csrf)
	if rec.Code != http.StatusOK {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestVolumeDeleteRequiresConfirmation(t *testing.T) {
	h, _ := newTestAPI(t)
	cookies, csrf := envSession(t, h)

	// Without ?confirm → 400.
	rec := envDo(t, h, "DELETE", "/api/v1/docker/volumes/some_vol", nil, cookies, csrf)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing confirm: %d %s", rec.Code, rec.Body.String())
	}

	// Wrong confirm → 400.
	rec = envDo(t, h, "DELETE", "/api/v1/docker/volumes/some_vol?confirm=other_vol", nil, cookies, csrf)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("wrong confirm: %d", rec.Code)
	}

	// Matching confirm → 204 (fake docker client returns nil).
	rec = envDo(t, h, "DELETE", "/api/v1/docker/volumes/some_vol?confirm=some_vol", nil, cookies, csrf)
	if rec.Code != http.StatusNoContent {
		t.Errorf("ok confirm: %d %s", rec.Code, rec.Body.String())
	}
}
