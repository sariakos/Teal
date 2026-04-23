package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// httpDoer is the subset of *http.Client the webhook dispatcher uses.
// Lets tests inject a recorder.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func defaultHTTPClient() httpDoer {
	return &http.Client{Timeout: 10 * time.Second}
}

// CodecPurposeWebhookOutbound is the Codec purpose used to derive the
// per-app HMAC secret for outbound webhook payloads. Distinct from
// `webhook.secret` (inbound GitHub) so the two key derivations can't
// alias.
const CodecPurposeWebhookOutbound = "webhook.outbound"

// WebhookAAD binds the outbound-webhook secret ciphertext to its app.
// Mirrors the AAD pattern used elsewhere.
func WebhookAAD(appID int64) string {
	return "app:" + strconv.FormatInt(appID, 10) + ":notify"
}

// WebhookPayload is the wire shape we POST to the user's URL. Stable;
// receivers parse it long after a deploy.
type WebhookPayload struct {
	Event      string                `json:"event"`      // "deploy.succeeded" | "deploy.failed"
	TS         time.Time             `json:"ts"`
	App        webhookAppPayload     `json:"app"`
	Deployment webhookDeployPayload  `json:"deployment"`
}

type webhookAppPayload struct {
	Slug    string   `json:"slug"`
	Name    string   `json:"name"`
	Domains []string `json:"domains,omitempty"`
}

type webhookDeployPayload struct {
	ID            int64     `json:"id"`
	Color         string    `json:"color"`
	Status        string    `json:"status"`
	CommitSHA     string    `json:"commitSha,omitempty"`
	TriggerKind   string    `json:"triggerKind,omitempty"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	CompletedAt   time.Time `json:"completedAt,omitempty"`
	FailureReason string    `json:"failureReason,omitempty"`
}

// deliverWebhook sends the event to App.NotificationWebhookURL. Retries
// up to 3 times with 1s/4s/16s backoff; any non-2xx response counts as
// a failure. Permanent failure lands an audit row + an in-app warning.
func (n *Notifier) deliverWebhook(ctx context.Context, evt Event) {
	body, err := buildWebhookPayload(evt)
	if err != nil {
		n.logger.Warn("notify: build payload", "app", evt.App.Slug, "err", err)
		return
	}

	sig, err := n.signPayload(evt.App, body)
	if err != nil {
		n.logger.Warn("notify: sign payload", "app", evt.App.Slug, "err", err)
		return
	}

	backoffs := []time.Duration{0, 1 * time.Second, 4 * time.Second, 16 * time.Second}
	for attempt, delay := range backoffs {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, "POST",
			evt.App.NotificationWebhookURL, bytes.NewReader(body))
		if err != nil {
			n.logger.Warn("notify: build request", "err", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "teal-notify/1")
		req.Header.Set("X-Teal-Event", string(payloadEvent(evt)))
		req.Header.Set("X-Teal-Signature", "sha256="+sig)

		resp, err := n.httpClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return // delivered
			}
			err = fmt.Errorf("status %d", resp.StatusCode)
		}
		n.logger.Debug("notify: webhook attempt failed",
			"app", evt.App.Slug, "attempt", attempt+1, "err", err)
		if attempt == len(backoffs)-1 {
			// Terminal — surface in-app + audit.
			n.afterWebhookFailure(ctx, evt, err)
		}
	}
}

func payloadEvent(evt Event) domain.NotificationKind {
	if evt.Failed {
		return domain.NotificationKindDeployFailed
	}
	return domain.NotificationKindDeploySucceeded
}

func buildWebhookPayload(evt Event) ([]byte, error) {
	app := webhookAppPayload{Slug: evt.App.Slug, Name: evt.App.Name}
	if evt.App.Domains != "" {
		// Reuse the API's split logic shape — comma-split, trim, drop empties.
		for _, d := range splitTrim(evt.App.Domains, ',') {
			if d != "" {
				app.Domains = append(app.Domains, d)
			}
		}
	}
	dep := webhookDeployPayload{
		ID: evt.Deployment.ID, Color: string(evt.Deployment.Color),
		Status: string(evt.Deployment.Status), CommitSHA: evt.Deployment.CommitSHA,
		TriggerKind: string(evt.Deployment.TriggerKind),
	}
	if evt.Deployment.StartedAt != nil {
		dep.StartedAt = *evt.Deployment.StartedAt
	}
	if evt.Deployment.CompletedAt != nil {
		dep.CompletedAt = *evt.Deployment.CompletedAt
	}
	if evt.Failed {
		dep.FailureReason = evt.Reason
	}
	return json.Marshal(WebhookPayload{
		Event: string(payloadEvent(evt)), TS: time.Now().UTC(),
		App: app, Deployment: dep,
	})
}

// signPayload returns the lowercase-hex HMAC-SHA256 of body keyed by
// the app's outbound webhook secret. Returns "" + error when no secret
// is stored — the dispatcher refuses to send unsigned bodies, which
// would let an attacker send fake events to the user's receiver.
func (n *Notifier) signPayload(app domain.App, body []byte) (string, error) {
	if len(app.NotificationWebhookSecretEncrypted) == 0 {
		return "", fmt.Errorf("no outbound webhook secret stored for app %q", app.Slug)
	}
	secret, err := n.codec.Open(CodecPurposeWebhookOutbound, WebhookAAD(app.ID), app.NotificationWebhookSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// afterWebhookFailure records the terminal failure: in-app row + audit.
func (n *Notifier) afterWebhookFailure(ctx context.Context, evt Event, last error) {
	_, _ = n.store.Notifications.Insert(ctx, domain.Notification{
		Level: domain.NotificationWarn, Kind: domain.NotificationKindWebhookFailed,
		Title:   "Webhook delivery failed: " + evt.App.Slug,
		Body:    fmt.Sprintf("Tried 4 times. Last error: %v", last),
		AppSlug: evt.App.Slug,
	})
	if n.hub != nil {
		n.hub.Publish("notifications.broadcast", map[string]any{
			"kind":    string(domain.NotificationKindWebhookFailed),
			"app":     evt.App.Slug,
			"message": last.Error(),
		})
	}
}

// splitTrim is a tiny helper local to this file — the api package has
// the same logic but importing it would create a cycle.
func splitTrim(s string, sep byte) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == sep {
			seg := s[start:i]
			for len(seg) > 0 && (seg[0] == ' ' || seg[0] == '\t') {
				seg = seg[1:]
			}
			for len(seg) > 0 && (seg[len(seg)-1] == ' ' || seg[len(seg)-1] == '\t') {
				seg = seg[:len(seg)-1]
			}
			out = append(out, seg)
			start = i + 1
		}
	}
	return out
}
