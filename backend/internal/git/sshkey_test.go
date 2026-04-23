package git

import (
	"crypto/ed25519"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateSSHKeyPairProducesValidOpenSSHKey(t *testing.T) {
	priv, pub, err := GenerateSSHKeyPair("myapp")
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair: %v", err)
	}

	// Private key parses as a valid OpenSSH PEM.
	block, _ := pem.Decode(priv)
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" {
		t.Fatalf("private key PEM block invalid: %v", block)
	}
	signer, err := ssh.ParsePrivateKey(priv)
	if err != nil {
		t.Fatalf("ssh.ParsePrivateKey: %v", err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("key type = %s, want ssh-ed25519", signer.PublicKey().Type())
	}

	// Public key line is "ssh-ed25519 <base64> teal@myapp".
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Errorf("public line wrong prefix: %q", pub)
	}
	if !strings.Contains(pub, "teal@myapp") {
		t.Errorf("public line missing label: %q", pub)
	}

	// Sign + verify round-trip with the parsed signer.
	msg := []byte("teal-test")
	sig, err := signer.Sign(nil, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := signer.PublicKey().Verify(msg, sig); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestSSHKeyFingerprint(t *testing.T) {
	_, pub, err := GenerateSSHKeyPair("x")
	if err != nil {
		t.Fatal(err)
	}
	fp, err := SSHKeyFingerprint(pub)
	if err != nil {
		t.Fatalf("SSHKeyFingerprint: %v", err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("fingerprint format unexpected: %q", fp)
	}
	// Compare to ssh.FingerprintSHA256 of the same parsed public key.
	parsed, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(pub))
	want := ssh.FingerprintSHA256(parsed)
	if fp != want {
		t.Errorf("fingerprint = %s, want %s", fp, want)
	}
}

func TestSSHKeyFingerprintRejectsMalformed(t *testing.T) {
	if _, err := SSHKeyFingerprint("not a key"); err == nil {
		t.Error("expected error for malformed input")
	}
}

// touch ed25519 import so it never goes unused if Generate is removed
var _ = ed25519.PublicKeySize
