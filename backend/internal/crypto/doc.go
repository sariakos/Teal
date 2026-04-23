// Package crypto holds Teal's symmetric-encryption primitives. Two callers
// today — the auth package (encrypted TOTP secret in Phase 2+) and the
// EnvVar repository (encrypted variable values, Phase 5) — both derive keys
// from the platform secret. Putting the helpers here means there is exactly
// one implementation of "turn the platform secret into a per-purpose key"
// and exactly one implementation of "encrypt/decrypt a blob".
//
// What it does:
//   - KeyFromSecret: HKDF-SHA256(secret, info=purpose) -> 32 bytes.
//   - Encrypt / Decrypt: AES-256-GCM over a key, with a fresh nonce per
//     ciphertext, returned as nonce||ciphertext||tag.
//
// What it does NOT do:
//   - Asymmetric crypto, signing, hashing for password storage. Passwords
//     are bcrypt-hashed in internal/auth, not here.
//   - Persistent key storage. The platform secret is the only long-lived
//     key material; everything else is derived deterministically from it.
package crypto
