package domain

import "time"

// NotificationLevel ranks how loudly the UI should surface a
// notification. Constant values are short so they fit in CSS class
// suffixes without translation.
type NotificationLevel string

const (
	NotificationInfo  NotificationLevel = "info"
	NotificationWarn  NotificationLevel = "warn"
	NotificationError NotificationLevel = "error"
)

// NotificationKind identifies what produced the notification. Values
// are stable for forensic queries; never repurpose.
type NotificationKind string

const (
	NotificationKindDeploySucceeded NotificationKind = "deploy.succeeded"
	NotificationKindDeployFailed    NotificationKind = "deploy.failed"
	NotificationKindWebhookFailed   NotificationKind = "webhook.failed"
	NotificationKindEmailFailed     NotificationKind = "email.failed"
	NotificationKindPlatformUpdate  NotificationKind = "platform.update_requested"
)

// Notification is one in-app feed entry. UserID == nil means broadcast
// (resolved at read time to "every admin"). AppSlug is empty when the
// notification isn't bound to a specific app.
type Notification struct {
	ID        int64
	UserID    *int64 // nil → broadcast to admins
	Level     NotificationLevel
	Kind      NotificationKind
	Title     string
	Body      string
	AppSlug   string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// NotifyTopic returns the realtime topic name for a user's
// notification feed. Centralised so backend + frontend agree.
func NotifyTopic(userID int64) string {
	return "notifications." + intStr(userID)
}

// intStr is a tiny wrapper so this package doesn't pull strconv just
// for one call. Keeps domain stdlib-free in spirit.
func intStr(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
