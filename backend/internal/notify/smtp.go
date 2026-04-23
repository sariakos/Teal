package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// smtpSender is the interface SMTP delivery uses. Production
// implementation reads platform_settings each call (admin can change
// the SMTP server without restart). Tests inject a recorder.
type smtpSender interface {
	Send(ctx context.Context, to []string, subject, body string) error
}

// platformSettingsSMTP composes a sender backed by the live KV table.
type platformSettingsSMTP struct {
	store *store.Store
}

func defaultSMTPSender(st *store.Store) smtpSender {
	return &platformSettingsSMTP{store: st}
}

// Settings keys consumed by SMTP. Admin sets these in
// /settings/platform; missing keys disable email entirely.
const (
	settingSMTPHost     = "smtp.host"
	settingSMTPPort     = "smtp.port"
	settingSMTPUser     = "smtp.user"
	settingSMTPPass     = "smtp.pass"
	settingSMTPFrom     = "smtp.from"
	settingSMTPStartTLS = "smtp.starttls" // "true" → upgrade plaintext to TLS
)

func (p *platformSettingsSMTP) Send(ctx context.Context, to []string, subject, body string) error {
	host, err := p.store.PlatformSettings.GetOrDefault(ctx, settingSMTPHost, "")
	if err != nil {
		return fmt.Errorf("read smtp host: %w", err)
	}
	if host == "" {
		return fmt.Errorf("smtp not configured")
	}
	port, _ := p.store.PlatformSettings.GetOrDefault(ctx, settingSMTPPort, "587")
	user, _ := p.store.PlatformSettings.GetOrDefault(ctx, settingSMTPUser, "")
	pass, _ := p.store.PlatformSettings.GetOrDefault(ctx, settingSMTPPass, "")
	from, _ := p.store.PlatformSettings.GetOrDefault(ctx, settingSMTPFrom, user)
	startTLS, _ := p.store.PlatformSettings.GetOrDefault(ctx, settingSMTPStartTLS, "true")

	addr := net.JoinHostPort(host, port)
	msg := buildSMTPMessage(from, to, subject, body)

	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Deadline: deadline}
	c, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()

	smtpClient, err := smtp.NewClient(c, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer smtpClient.Close()

	if startTLS == "true" {
		if err := smtpClient.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if user != "" {
		auth := smtp.PlainAuth("", user, pass, host)
		if err := smtpClient.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := smtpClient.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := smtpClient.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %q: %w", rcpt, err)
		}
	}
	wc, err := smtpClient.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("end DATA: %w", err)
	}
	return smtpClient.Quit()
}

// buildSMTPMessage produces a minimal RFC 5322 message. Plain-text
// only; HTML is overkill for failure pings.
func buildSMTPMessage(from string, to []string, subject, body string) []byte {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return []byte(sb.String())
}

// deliverEmail sends the failure email. SMTP-not-configured is logged
// at debug (not warn — many installs intentionally skip SMTP).
func (n *Notifier) deliverEmail(ctx context.Context, evt Event) {
	if n.smtpSender == nil {
		return
	}
	subject := "[Teal] Deploy failed: " + evt.App.Slug
	body := fmt.Sprintf(`Deployment %d for app %q (color %s) failed.

Reason: %s

Commit: %s
Trigger: %s
`, evt.Deployment.ID, evt.App.Slug, evt.Deployment.Color, evt.Reason,
		evt.Deployment.CommitSHA, evt.Deployment.TriggerKind)

	if err := n.smtpSender.Send(ctx, []string{evt.App.NotificationEmail}, subject, body); err != nil {
		if strings.Contains(err.Error(), "smtp not configured") {
			n.logger.Debug("notify: email skipped (smtp not configured)", "app", evt.App.Slug)
			return
		}
		n.logger.Warn("notify: email send failed", "app", evt.App.Slug, "err", err)
		_, _ = n.store.Notifications.Insert(ctx, domain.Notification{
			Level: domain.NotificationWarn, Kind: domain.NotificationKindEmailFailed,
			Title: "Email delivery failed: " + evt.App.Slug,
			Body:  err.Error(), AppSlug: evt.App.Slug,
		})
	}
}
