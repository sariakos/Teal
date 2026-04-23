package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// PasswordCost is the bcrypt cost used for new password hashes. The spec
// mandates ≥ 12. Higher costs slow the login path quadratically; 12 is the
// floor that gives ~250ms on commodity hardware in 2026, which is well
// inside acceptable login latency.
const PasswordCost = 12

// MinPasswordLength is the floor enforced at hash time. The frontend should
// also enforce it (with friendlier messages) but the server is the source of
// truth.
const MinPasswordLength = 12

// ErrPasswordTooShort is returned by HashPassword when the input is below
// MinPasswordLength.
var ErrPasswordTooShort = errors.New("auth: password too short")

// ErrPasswordMismatch is returned by ComparePassword when the password does
// not match the stored hash. It is intentionally distinct from
// bcrypt.ErrMismatchedHashAndPassword so callers do not need to import the
// bcrypt package to recognise an auth failure.
var ErrPasswordMismatch = errors.New("auth: password mismatch")

// HashPassword returns a bcrypt hash of password at PasswordCost. The output
// is the standard $2a$… string encoded as bytes — store it directly.
func HashPassword(password string) ([]byte, error) {
	if len(password) < MinPasswordLength {
		return nil, ErrPasswordTooShort
	}
	return bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
}

// ComparePassword reports whether the password matches the stored hash. It
// is constant-time with respect to whether the hash exists; callers should
// still avoid enumerating users via response timing by performing a dummy
// compare on missing-user lookups (see api/auth.go).
func ComparePassword(hash []byte, password string) error {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err == nil {
		return nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrPasswordMismatch
	}
	return err
}
