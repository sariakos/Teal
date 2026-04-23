package domain

import "time"

// Session is one active browser-based authentication for a User. The cookie
// carries the opaque ID; the CSRF token is rotated per session and bound to
// it. Sessions have a sliding expiry: LastSeenAt is bumped on use, and the
// auth middleware extends ExpiresAt forward as long as the session is
// active.
//
// Server-side storage means revocation is immediate (delete the row).
type Session struct {
	ID         string // opaque, base32 of 32 random bytes
	UserID     int64
	CSRFToken  string // base32 of 32 random bytes; rotated on issue
	IP         string
	UserAgent  string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

// IsExpired reports whether the session is past its ExpiresAt at the given
// instant.
func (s Session) IsExpired(at time.Time) bool {
	return !at.Before(s.ExpiresAt)
}
