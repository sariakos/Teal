package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestKeyFromSecretIsDeterministic(t *testing.T) {
	secret := []byte("super secret platform secret value here")
	a, err := KeyFromSecret(secret, "test.purpose")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := KeyFromSecret(secret, "test.purpose")
	if !bytes.Equal(a, b) {
		t.Error("same inputs should yield same key")
	}
	if len(a) != KeySize {
		t.Errorf("key length = %d, want %d", len(a), KeySize)
	}
}

func TestKeyFromSecretPurposeMatters(t *testing.T) {
	secret := []byte("super secret platform secret value here")
	a, _ := KeyFromSecret(secret, "purpose.one")
	b, _ := KeyFromSecret(secret, "purpose.two")
	if bytes.Equal(a, b) {
		t.Error("different purposes must yield different keys")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, _ := KeyFromSecret([]byte("a secret long enough to be useful"), "test")
	plaintext := []byte("hello, world")

	ct, err := Encrypt(key, plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Error("ciphertext equals plaintext (encryption did nothing)")
	}

	pt, err := Decrypt(key, ct, nil)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("round-trip lost data: got %q, want %q", pt, plaintext)
	}
}

func TestEncryptProducesUniqueCiphertexts(t *testing.T) {
	key, _ := KeyFromSecret([]byte("secret"), "test")
	a, _ := Encrypt(key, []byte("same"), nil)
	b, _ := Encrypt(key, []byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Error("two encryptions of the same plaintext must differ (nonce reuse?)")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	key, _ := KeyFromSecret([]byte("secret"), "test")
	ct, _ := Encrypt(key, []byte("hello"), nil)

	tampered := append([]byte{}, ct...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := Decrypt(key, tampered, nil); err == nil {
		t.Error("Decrypt accepted tampered ciphertext")
	}
}

func TestDecryptRejectsWrongAssociatedData(t *testing.T) {
	key, _ := KeyFromSecret([]byte("secret"), "test")
	ct, _ := Encrypt(key, []byte("hello"), []byte("ctx-A"))
	if _, err := Decrypt(key, ct, []byte("ctx-B")); err == nil {
		t.Error("Decrypt accepted mismatched associated data")
	}
}

func TestDecryptRejectsShortCiphertext(t *testing.T) {
	key, _ := KeyFromSecret([]byte("secret"), "test")
	_, err := Decrypt(key, []byte{1, 2, 3}, nil)
	if !errors.Is(err, ErrCiphertextTooShort) {
		t.Errorf("want ErrCiphertextTooShort, got %v", err)
	}
}

func TestEncryptRejectsBadKeyLen(t *testing.T) {
	_, err := Encrypt([]byte("too short"), []byte("x"), nil)
	if err == nil {
		t.Error("Encrypt accepted short key")
	}
}
