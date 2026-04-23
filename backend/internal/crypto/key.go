package crypto

import (
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// KeyFromSecret derives a 32-byte key from the platform secret using
// HKDF-SHA256. The purpose string is used as the HKDF "info" parameter so
// that two different callers (e.g. "envvar.value", "user.totp") deriving from
// the same secret get different keys, even though the inputs are otherwise
// identical.
//
// The salt parameter is empty by design: we do not have a per-instance salt
// to thread through and HKDF without a salt is well-defined (RFC 5869
// §2.2 — "if not provided, it is set to a string of HashLen zeros").
//
// purpose must be a stable, ASCII string. Changing it for an existing
// caller is equivalent to losing the key — never do it without a migration.
func KeyFromSecret(secret []byte, purpose string) ([]byte, error) {
	r := hkdf.New(sha256.New, secret, nil /* salt */, []byte(purpose))
	out := make([]byte, KeySize)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}
