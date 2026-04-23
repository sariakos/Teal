package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SignatureHeader is the header GitHub sends with each webhook delivery.
// It carries the SHA-256 HMAC of the raw request body, prefixed with
// "sha256=".
const SignatureHeader = "X-Hub-Signature-256"

// EventHeader carries GitHub's event name (e.g. "push", "ping").
const EventHeader = "X-GitHub-Event"

// DeliveryHeader is GitHub's per-delivery UUID; logged for diagnostics.
const DeliveryHeader = "X-GitHub-Delivery"

// VerifySignature reports whether headerValue is a valid HMAC-SHA256 of body
// under secret. headerValue must include the "sha256=" prefix per GitHub's
// format.
//
// The comparison uses hmac.Equal (constant time) so a partial-match attacker
// cannot probe one byte at a time.
func VerifySignature(secret, body []byte, headerValue string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(headerValue, prefix) {
		return false
	}
	expected := strings.TrimPrefix(headerValue, prefix)
	want, err := hex.DecodeString(expected)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(want, got)
}

// Sign produces the value that should appear in SignatureHeader for body
// signed with secret. Used by tests; production code only validates.
func Sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
