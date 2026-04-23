package github

import "testing"

func TestSignAndVerifyRoundTrip(t *testing.T) {
	secret := []byte("super-secret")
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := Sign(secret, body)
	if !VerifySignature(secret, body, sig) {
		t.Errorf("Verify failed for valid signature")
	}
}

func TestVerifySignatureRejectsTamper(t *testing.T) {
	secret := []byte("super-secret")
	body := []byte(`hello`)
	sig := Sign(secret, body)
	if VerifySignature(secret, []byte("HELLO"), sig) {
		t.Error("verify accepted modified body")
	}
	if VerifySignature([]byte("other"), body, sig) {
		t.Error("verify accepted wrong secret")
	}
}

func TestVerifySignatureRejectsBadFormat(t *testing.T) {
	secret := []byte("k")
	body := []byte("x")
	cases := []string{
		"",
		"sha1=deadbeef",
		"sha256=not-hex",
		"sha256=",
	}
	for _, c := range cases {
		if VerifySignature(secret, body, c) {
			t.Errorf("verify accepted malformed header %q", c)
		}
	}
}

// Fixed-vector test against a known body+secret so future refactors don't
// silently change the algorithm.
func TestSignKnownVector(t *testing.T) {
	got := Sign([]byte("It's a Secret to Everybody"), []byte("Hello, World!"))
	want := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
