// Package notify dispatches outbound deployment notifications: per-app
// HTTP webhooks (HMAC-signed), optional SMTP failure emails, and
// in-app feed entries that the bell renders.
//
// One Notifier per process. The deploy engine calls
// OnDeploymentFinished after every terminal status (succeeded /
// failed). Each enabled channel runs in a fresh goroutine so a slow
// SMTP server never blocks the deploy loop.
package notify

import (
	"context"
	"log/slog"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// Publisher is the subset of the realtime hub the notifier uses (to
// push live notifications.<user-id> events). Defined here so tests can
// pass a no-op without dragging in the hub.
type Publisher interface {
	Publish(topic string, data any)
}

// Event is the canonical shape passed to OnDeploymentFinished. Holds
// just enough that downstream channels can render their messages
// without re-querying the store.
type Event struct {
	App        domain.App
	Deployment domain.Deployment
	Failed     bool
	Reason     string // app-meaningful text; the failure_reason or empty
}

// Notifier dispatches Events to every enabled channel.
type Notifier struct {
	logger *slog.Logger
	store  *store.Store
	codec  *crypto.Codec
	hub    Publisher

	// httpClient lets tests inject a stub. Default is set in New.
	httpClient httpDoer
	smtpSender smtpSender
}

// New constructs a Notifier with production defaults: real HTTP client
// + real SMTP sender that reads from platform_settings.
func New(logger *slog.Logger, st *store.Store, codec *crypto.Codec, hub Publisher) *Notifier {
	return &Notifier{
		logger:     logger,
		store:      st,
		codec:      codec,
		hub:        hub,
		httpClient: defaultHTTPClient(),
		smtpSender: defaultSMTPSender(st),
	}
}

// SetHTTPClient swaps the webhook-dispatching client. Used by tests.
func (n *Notifier) SetHTTPClient(c httpDoer) { n.httpClient = c }

// SetSMTPSender swaps the SMTP backend. Used by tests.
func (n *Notifier) SetSMTPSender(s smtpSender) { n.smtpSender = s }

// OnDeploymentFinished runs every enabled channel for the event. Each
// channel runs in a fresh goroutine and logs its own errors; this
// method returns immediately so the engine's deploy goroutine isn't
// blocked.
func (n *Notifier) OnDeploymentFinished(ctx context.Context, evt Event) {
	// In-app feed first (cheapest, gives the user a UI signal even when
	// outbound channels are slow or misconfigured).
	go n.deliverInApp(context.Background(), evt)

	if evt.App.NotificationWebhookURL != "" {
		go n.deliverWebhook(context.Background(), evt)
	}
	if evt.Failed && evt.App.NotificationEmail != "" {
		go n.deliverEmail(context.Background(), evt)
	}
}

// deliverInApp inserts a notification row + publishes on the hub topic
// `notifications.broadcast` (the bell subscribes per-user OR to the
// broadcast topic when the user is admin).
func (n *Notifier) deliverInApp(ctx context.Context, evt Event) {
	level := domain.NotificationInfo
	kind := domain.NotificationKindDeploySucceeded
	title := "Deploy succeeded: " + evt.App.Slug
	body := ""
	if evt.Failed {
		level = domain.NotificationError
		kind = domain.NotificationKindDeployFailed
		title = "Deploy failed: " + evt.App.Slug
		body = evt.Reason
	}
	row := domain.Notification{
		Level:   level,
		Kind:    kind,
		Title:   title,
		Body:    body,
		AppSlug: evt.App.Slug,
	}
	saved, err := n.store.Notifications.Insert(ctx, row)
	if err != nil {
		n.logger.Warn("notify: persist in-app failed", "app", evt.App.Slug, "err", err)
		return
	}
	if n.hub != nil {
		// Broadcast topic — the bell subscribes here for any
		// non-user-targeted entry. (Per-user targeted notifications use
		// domain.NotifyTopic(userID).)
		n.hub.Publish("notifications.broadcast", saved)
	}
}
