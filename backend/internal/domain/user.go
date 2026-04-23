package domain

import "time"

// UserRole controls what a User is allowed to do. The hierarchy is strict:
// admin > member > viewer. Permission checks compare ordinals, not names.
type UserRole string

const (
	UserRoleViewer UserRole = "viewer" // read-only
	UserRoleMember UserRole = "member" // can manage and deploy apps
	UserRoleAdmin  UserRole = "admin"  // can manage users and platform settings
)

// User is a local account on this Teal instance. Authentication is always
// against this table — there is no external identity provider.
//
// Sensitive fields:
//   - PasswordHash is a bcrypt digest (cost ≥ 12). Never log it, never
//     return it via the API.
//   - TOTPSecretEncrypted is encrypted with the platform secret. Decrypt only
//     during 2FA verification.
type User struct {
	ID    int64
	Email string

	PasswordHash []byte
	Role         UserRole

	// TOTPSecretEncrypted is empty when 2FA is not enrolled. Stored as
	// AES-256-GCM ciphertext (nonce prefix + ciphertext + tag).
	TOTPSecretEncrypted []byte

	CreatedAt time.Time
	UpdatedAt time.Time
}
