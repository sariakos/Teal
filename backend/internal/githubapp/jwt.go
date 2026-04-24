// Package githubapp implements GitHub App authentication: JWT minting
// (RS256, signed with the App's private key), installation-token fetch
// from GitHub's API, and a small in-memory cache so we don't request a
// fresh token on every git operation.
//
// What it does NOT do:
//   - Manifest-flow App registration (admin creates the App manually
//     and pastes credentials into platform settings).
//   - OAuth user-token issuance (Teal authenticates as the App, never
//     as a user).
//   - GitHub Enterprise endpoints (hardcoded https://api.github.com).
package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// AppJWTTTL is how long a freshly-minted App JWT is valid. GitHub
// caps it at 10 minutes; we use 9 to leave clock-skew headroom.
const AppJWTTTL = 9 * time.Minute

// MintAppJWT builds a JWT signed with the App's RSA private key. Used
// to authenticate as the App when requesting installation tokens.
//
// The PEM is parsed each call (cheap; ~µs); higher layers cache the
// resulting installation token, not this JWT.
func MintAppJWT(appID int64, privateKeyPEM []byte, now time.Time) (string, error) {
	if appID <= 0 {
		return "", errors.New("githubapp: app ID required")
	}
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("githubapp: parse private key: %w", err)
	}

	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	// GitHub backdates `iat` by 60s (their docs) to tolerate clock skew
	// in the other direction; `exp` is 9 min ahead per AppJWTTTL.
	payload := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(AppJWTTTL).Unix(),
		"iss": appID,
	}

	headerB, err := jsonAndEncode(header)
	if err != nil {
		return "", err
	}
	payloadB, err := jsonAndEncode(payload)
	if err != nil {
		return "", err
	}
	signingInput := headerB + "." + payloadB

	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("githubapp: sign jwt: %w", err)
	}
	sigB := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigB, nil
}

// jsonAndEncode marshals v to JSON, then base64url-encodes (no padding)
// per the JWT spec.
func jsonAndEncode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// parseRSAPrivateKey accepts either a PKCS#1 ("RSA PRIVATE KEY") or
// PKCS#8 ("PRIVATE KEY") encoded RSA key. GitHub's private-key download
// is PKCS#1; some operators paste a re-encoded PKCS#8 — accepting both
// avoids surprising "key parse failed" errors at deploy time.
func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		raw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		k, ok := raw.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is %T, want *rsa.PrivateKey", raw)
		}
		return k, nil
	}
	return nil, fmt.Errorf("unsupported PEM type %q (want RSA PRIVATE KEY or PRIVATE KEY)", block.Type)
}
