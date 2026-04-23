package domain

import "time"

// APIKey is one programmatic credential issued for a User. Only the SHA-256
// of the raw key is stored; the raw form is shown once at creation and never
// again.
//
// Revocation is recorded as a timestamp rather than a deletion so that the
// audit log can still link to the row that authorised a past action.
type APIKey struct {
	ID         int64
	UserID     int64
	Name       string // human label, e.g. "ci-deploy"
	KeyHash    []byte // 32-byte SHA-256 of the raw key
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// IsRevoked reports whether the key has been revoked.
func (k APIKey) IsRevoked() bool {
	return k.RevokedAt != nil
}
