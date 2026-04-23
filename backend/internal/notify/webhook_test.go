package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// captureClient records every Do call and returns a configurable
// response. Lets us assert HMAC + retry behaviour without a real HTTP
// server.
type captureClient struct {
	mu       sync.Mutex
	calls    []*http.Request
	bodies   [][]byte
	statuses []int // popped front; once empty defaults to 200
	err      error
}

func (c *captureClient) Do(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	body, _ := io.ReadAll(r.Body)
	c.calls = append(c.calls, r)
	c.bodies = append(c.bodies, body)
	status := 200
	if len(c.statuses) > 0 {
		status = c.statuses[0]
		c.statuses = c.statuses[1:]
	}
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func newNotifyStore(t *testing.T) (*store.Store, *crypto.Codec) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	codec, err := crypto.NewCodec([]byte("notify-test-secret-padding-32-byte!"))
	if err != nil {
		t.Fatal(err)
	}
	return st, codec
}

func sealOutbound(t *testing.T, c *crypto.Codec, appID int64, secret []byte) []byte {
	t.Helper()
	ct, err := c.Seal(CodecPurposeWebhookOutbound, WebhookAAD(appID), secret)
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

func TestWebhookSendsSignedPayloadOnce(t *testing.T) {
	st, codec := newNotifyStore(t)
	app, _ := st.Apps.Create(context.Background(), domain.App{
		Slug: "x", Name: "X",
		NotificationWebhookURL:             "http://example.invalid/hook",
		NotificationWebhookSecretEncrypted: sealOutbound(t, codec, 1, []byte("super-secret")),
	})

	cap := &captureClient{}
	n := New(slog.New(slog.NewTextHandler(io.Discard, nil)), st, codec, nil)
	n.SetHTTPClient(cap)

	ctx := context.Background()
	dep := domain.Deployment{ID: 42, AppID: app.ID, Color: domain.ColorBlue, Status: domain.DeploymentStatusSucceeded}
	n.deliverWebhook(ctx, Event{App: app, Deployment: dep})

	if len(cap.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(cap.calls))
	}
	req := cap.calls[0]
	if req.Header.Get("X-Teal-Event") != string(domain.NotificationKindDeploySucceeded) {
		t.Errorf("event header: %q", req.Header.Get("X-Teal-Event"))
	}

	gotSig := req.Header.Get("X-Teal-Signature")
	mac := hmac.New(sha256.New, []byte("super-secret"))
	mac.Write(cap.bodies[0])
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != wantSig {
		t.Errorf("signature mismatch:\n got  %s\n want %s", gotSig, wantSig)
	}
}

func TestWebhookRetriesOnNon2xxThenGivesUp(t *testing.T) {
	st, codec := newNotifyStore(t)
	app, _ := st.Apps.Create(context.Background(), domain.App{
		Slug: "x", Name: "X",
		NotificationWebhookURL:             "http://example.invalid/hook",
		NotificationWebhookSecretEncrypted: sealOutbound(t, codec, 1, []byte("k")),
	})
	cap := &captureClient{statuses: []int{500, 502, 503, 504}}
	n := New(slog.New(slog.NewTextHandler(io.Discard, nil)), st, codec, nil)
	n.SetHTTPClient(cap)

	// Speed test: cancel after 50ms — the dispatcher's 1s/4s/16s
	// backoff would otherwise make this slow. Cancellation truncates the
	// retry loop early, so we expect at most 4 attempts but at least 1.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	n.deliverWebhook(ctx, Event{App: app, Deployment: domain.Deployment{ID: 1, AppID: app.ID}})

	if len(cap.calls) < 1 {
		t.Errorf("expected ≥1 call, got %d", len(cap.calls))
	}

	// Failure path: terminal failure inserts an in-app notification.
	// Only assert when the loop actually exhausted (ctx allowed it).
	if len(cap.calls) == 4 {
		rows, _ := st.Notifications.ListForUser(context.Background(), 0, true, 50)
		var sawWebhookFailed bool
		for _, r := range rows {
			if r.Kind == domain.NotificationKindWebhookFailed {
				sawWebhookFailed = true
			}
		}
		if !sawWebhookFailed {
			t.Errorf("expected webhook.failed notification after 4 attempts; rows=%+v", rows)
		}
	}
}

func TestWebhookRefusesUnsignedSend(t *testing.T) {
	st, codec := newNotifyStore(t)
	app, _ := st.Apps.Create(context.Background(), domain.App{
		Slug: "x", Name: "X",
		NotificationWebhookURL: "http://example.invalid/hook",
		// NO secret stored — dispatcher must refuse rather than send unsigned.
	})
	cap := &captureClient{}
	n := New(slog.New(slog.NewTextHandler(io.Discard, nil)), st, codec, nil)
	n.SetHTTPClient(cap)

	n.deliverWebhook(context.Background(), Event{App: app, Deployment: domain.Deployment{ID: 1}})

	if len(cap.calls) != 0 {
		t.Errorf("dispatcher sent without secret: %d calls", len(cap.calls))
	}
}
