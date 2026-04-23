package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrCiphertextTooShort is returned by Decrypt when the input is shorter
// than the nonce length, i.e. it cannot have been produced by Encrypt.
var ErrCiphertextTooShort = errors.New("crypto: ciphertext too short")

// Encrypt seals plaintext under key (which must be exactly KeySize bytes)
// using AES-256-GCM with a freshly generated nonce. The output layout is:
//
//	output = nonce || ciphertext || tag
//
// where nonce is 12 bytes (the standard GCM size) and the tag is 16 bytes
// appended by the GCM Seal call. This single-blob format means callers
// don't need to track the nonce separately — Decrypt reconstructs it.
//
// associatedData (optional, may be nil) is bound into the GCM tag but not
// encrypted. Use it to bind the ciphertext to a context (e.g. a row's primary
// key) so swapping ciphertexts between contexts is detected.
func Encrypt(key, plaintext, associatedData []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: read nonce: %w", err)
	}
	// Seal appends to its first arg, so passing `nonce` puts the nonce at the
	// front of the output. The slice header from `nonce` is then the start of
	// the output blob, which is exactly the format we want.
	return gcm.Seal(nonce, nonce, plaintext, associatedData), nil
}

// Decrypt opens a ciphertext produced by Encrypt. associatedData must match
// what was passed to Encrypt; otherwise the GCM authentication fails and
// Decrypt returns an error without revealing the plaintext.
func Decrypt(key, ciphertext, associatedData []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, ErrCiphertextTooShort
	}
	nonce, body := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, body, associatedData)
	if err != nil {
		return nil, fmt.Errorf("crypto: open: %w", err)
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: key must be %d bytes (got %d)", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
