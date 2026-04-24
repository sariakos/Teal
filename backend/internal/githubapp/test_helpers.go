package githubapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

// Test-only helpers. Lowercase wrappers around the encoding/HMAC
// patterns SignState uses, so state_test can construct expired-but-
// valid-signature tokens without copy-pasting the cryptography.

func base64URLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func hmacBytes(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func jsonMarshalState(c StateClaims) ([]byte, error) {
	return json.Marshal(c)
}
