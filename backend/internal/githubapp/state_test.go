package githubapp

import (
	"strings"
	"testing"
	"time"
)

func TestSignAndParseStateRoundTrip(t *testing.T) {
	secret := []byte("test-secret-32-bytes-padding-..!")
	state, err := SignState(secret, "myapp", 7)
	if err != nil {
		t.Fatalf("SignState: %v", err)
	}
	c, err := ParseState(secret, state)
	if err != nil {
		t.Fatalf("ParseState: %v", err)
	}
	if c.Slug != "myapp" || c.UserID != 7 {
		t.Errorf("claims: %+v", c)
	}
	if c.Nonce == "" {
		t.Error("nonce should be populated")
	}
}

func TestParseStateRejectsTamperedSig(t *testing.T) {
	secret := []byte("test-secret-32-bytes-padding-..!")
	state, _ := SignState(secret, "myapp", 1)
	// Flip the last byte of the signature half. Pick a different
	// character than the existing one so the swap actually changes the
	// signature (otherwise the test is a no-op).
	parts := strings.Split(state, ".")
	last := parts[1][len(parts[1])-1]
	flipped := byte('A')
	if last == 'A' {
		flipped = 'B'
	}
	tampered := parts[0] + "." + parts[1][:len(parts[1])-1] + string(flipped)
	if _, err := ParseState(secret, tampered); err == nil {
		t.Error("expected tampered state to fail")
	}
}

func TestParseStateRejectsWrongSecret(t *testing.T) {
	state, _ := SignState([]byte("secret-A-32-bytes-padding-pad-x"), "x", 1)
	if _, err := ParseState([]byte("secret-B-32-bytes-padding-pad-y"), state); err == nil {
		t.Error("expected wrong-secret to fail")
	}
}

func TestParseStateRejectsExpired(t *testing.T) {
	secret := []byte("test-secret-32-bytes-padding-..!")
	// Build an explicitly-expired state and verify ParseState catches
	// it. Easier than waiting 10 minutes.
	c := StateClaims{Slug: "x", Nonce: "n", Expires: time.Now().Add(-1 * time.Hour).Unix()}
	state, err := signWith(secret, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseState(secret, state); err == nil {
		t.Error("expected expired state to fail")
	}
}

// signWith is a test-only helper: signs an explicit StateClaims so the
// test can produce already-expired tokens.
func signWith(secret []byte, c StateClaims) (string, error) {
	body, err := jsonMarshalState(c)
	if err != nil {
		return "", err
	}
	return base64URLEncode(body) + "." + base64URLEncode(hmacBytes(secret, body)), nil
}

func TestInstallURLFormat(t *testing.T) {
	got := InstallURL("teal-platform", "abc.def")
	want := "https://github.com/apps/teal-platform/installations/new?state=abc.def"
	if got != want {
		t.Errorf("InstallURL = %q, want %q", got, want)
	}
}

func TestInstallURLEmptySlug(t *testing.T) {
	if InstallURL("", "x") != "" {
		t.Error("empty slug should return empty URL")
	}
}
