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
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/github"
	"github.com/sariakos/teal/backend/internal/store"
)

// newTestAPIWithEngine reuses the auth-integration fake docker client but
// attaches a real Engine + Codec so the webhook flow is exercised
// end-to-end. The engine won't actually run docker compose because no
// deploy is triggered in these tests — we only check the gating logic up
// to the Trigger call.
func newTestAPIWithEngine(t *testing.T) (http.Handler, *store.Store, *crypto.Codec) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "teal.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	codec, err := crypto.NewCodec([]byte("some-long-enough-platform-secret-xyz"))
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := deploy.NewWithCodec(logger, st, fakeDockerClient{}, deploy.EngineConfig{
		WorkdirRoot:       t.TempDir(),
		TraefikDynamicDir: t.TempDir(),
	}, codec)

	authn := &auth.Authenticator{
		Sessions: auth.NewSessionManager(st.Sessions, false),
		APIKeys:  auth.NewAPIKeyManager(st.APIKeys),
		Users:    st.Users,
	}
	deps := Deps{
		Logger:        logger,
		Store:         st,
		Docker:        fakeDockerClient{},
		Authenticator: authn,
		RateLimiter:   auth.NewLoginRateLimiter(50, time.Minute),
		Engine:        engine,
		Codec:         codec,
	}
	return newRouter(deps), st, codec
}

// seedAppWithGit creates an app with auto_deploy enabled, stores a webhook
// secret, and returns the app + raw secret.
func seedAppWithGit(t *testing.T, st *store.Store, codec *crypto.Codec, slug, branch string) (domain.App, string) {
	t.Helper()
	ctx := context.Background()
	app, err := st.Apps.Create(ctx, domain.App{
		Slug: slug, Name: slug,
		GitURL: "https://example.com/x/y.git", GitBranch: branch,
		AutoDeployBranch: branch, AutoDeployEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "test-webhook-secret-32-bytes-padd"
	enc, err := codec.Seal("webhook.secret", "app:"+itoa(app.ID), []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	app.WebhookSecretEncrypted = enc
	if err := st.Apps.Update(ctx, app); err != nil {
		t.Fatal(err)
	}
	return app, secret
}

func pushBody(branch, sha string) []byte {
	b, _ := json.Marshal(map[string]any{
		"ref":         "refs/heads/" + branch,
		"head_commit": map[string]string{"id": sha},
		"repository":  map[string]string{"full_name": "owner/repo"},
	})
	return b
}

func TestWebhookValidSignatureTriggersDeploy(t *testing.T) {
	h, st, codec := newTestAPIWithEngine(t)
	app, secret := seedAppWithGit(t, st, codec, "demo", "main")

	body := pushBody("main", "abc1234567")
	sig := github.Sign([]byte(secret), body)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/github/demo", bytes.NewReader(body))
	req.Header.Set(github.SignatureHeader, sig)
	req.Header.Set(github.EventHeader, "push")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	deps, _ := st.Deployments.ListForApp(context.Background(), app.ID, 10)
	if len(deps) != 1 {
		t.Fatalf("want 1 deployment, got %d", len(deps))
	}
	if deps[0].TriggerKind != domain.TriggerWebhook {
		t.Errorf("trigger_kind = %q, want webhook", deps[0].TriggerKind)
	}
	if deps[0].CommitSHA != "abc1234567" {
		t.Errorf("commit sha = %q", deps[0].CommitSHA)
	}
}

func TestWebhookInvalidSignatureReturns401(t *testing.T) {
	h, st, codec := newTestAPIWithEngine(t)
	seedAppWithGit(t, st, codec, "demo", "main")

	body := pushBody("main", "abc")
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github/demo", bytes.NewReader(body))
	req.Header.Set(github.SignatureHeader, "sha256=deadbeef")
	req.Header.Set(github.EventHeader, "push")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookUnknownSlugReturns401(t *testing.T) {
	h, _, _ := newTestAPIWithEngine(t)
	body := pushBody("main", "abc")
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github/nope", bytes.NewReader(body))
	req.Header.Set(github.SignatureHeader, github.Sign([]byte("any"), body))
	req.Header.Set(github.EventHeader, "push")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown slug: status = %d, want 401 (no leak)", rec.Code)
	}
}

func TestWebhookBranchMismatchIgnored(t *testing.T) {
	h, st, codec := newTestAPIWithEngine(t)
	seedAppWithGit(t, st, codec, "demo", "main")

	body := pushBody("release/v1", "abc")
	sig := github.Sign([]byte("test-webhook-secret-32-bytes-padd"), body)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/github/demo", bytes.NewReader(body))
	req.Header.Set(github.SignatureHeader, sig)
	req.Header.Set(github.EventHeader, "push")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (ignored)", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("branch does not match")) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestWebhookAutoDeployDisabledIgnored(t *testing.T) {
	h, st, codec := newTestAPIWithEngine(t)
	app, secret := seedAppWithGit(t, st, codec, "demo", "main")
	app.AutoDeployEnabled = false
	if err := st.Apps.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}

	body := pushBody("main", "abc")
	sig := github.Sign([]byte(secret), body)
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github/demo", bytes.NewReader(body))
	req.Header.Set(github.SignatureHeader, sig)
	req.Header.Set(github.EventHeader, "push")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (ignored)", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("auto-deploy disabled")) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestWebhookPingEventRespondsPong(t *testing.T) {
	h, st, codec := newTestAPIWithEngine(t)
	_, secret := seedAppWithGit(t, st, codec, "demo", "main")

	body := []byte(`{"zen":"keep-it-simple"}`)
	sig := github.Sign([]byte(secret), body)
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github/demo", bytes.NewReader(body))
	req.Header.Set(github.SignatureHeader, sig)
	req.Header.Set(github.EventHeader, "ping")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("pong")) {
		t.Errorf("body = %s", rec.Body.String())
	}
}
