package githubapp

import (
	"encoding/base64"

	"github.com/sariakos/teal/backend/internal/crypto"
)

// Encrypted secrets live in the platform_settings KV table, which only
// holds TEXT. Wrap the AEAD ciphertext in base64 so we can round-trip.

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func decodeAndOpen(codec *crypto.Codec, purpose, encoded string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return codec.Open(purpose, aad, ct)
}
