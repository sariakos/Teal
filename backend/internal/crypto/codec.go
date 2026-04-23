package crypto

import (
	"fmt"
	"sync"
)

// Codec bundles the platform secret with a small cache of HKDF-derived
// per-purpose keys, and offers Encrypt/Decrypt with a uniform shape:
//
//	codec.Seal("git.private_key", "app:42", plaintext)
//	codec.Open("git.private_key", "app:42", ciphertext)
//
// The associatedData (passed as a string for ergonomics) binds the
// ciphertext to a context — swapping ciphertexts between contexts fails
// to authenticate. Standard pattern for at-rest encryption.
type Codec struct {
	secret []byte

	mu   sync.Mutex
	keys map[string][]byte
}

// NewCodec constructs a Codec from the platform secret. Returns an error
// if the secret is too short to satisfy the platform's invariants (32-byte
// minimum; same rule as config.Load enforces).
func NewCodec(secret []byte) (*Codec, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("crypto: codec needs ≥ 32-byte secret (got %d)", len(secret))
	}
	return &Codec{
		secret: append([]byte(nil), secret...),
		keys:   make(map[string][]byte),
	}, nil
}

// keyFor derives (or returns the cached) key for purpose.
func (c *Codec) keyFor(purpose string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if k, ok := c.keys[purpose]; ok {
		return k, nil
	}
	k, err := KeyFromSecret(c.secret, purpose)
	if err != nil {
		return nil, err
	}
	c.keys[purpose] = k
	return k, nil
}

// Seal encrypts plaintext under the purpose's derived key, with aad bound
// into the GCM tag.
func (c *Codec) Seal(purpose, aad string, plaintext []byte) ([]byte, error) {
	k, err := c.keyFor(purpose)
	if err != nil {
		return nil, err
	}
	return Encrypt(k, plaintext, []byte(aad))
}

// Open decrypts ciphertext under the purpose's derived key, requiring aad
// to match what was passed to Seal.
func (c *Codec) Open(purpose, aad string, ciphertext []byte) ([]byte, error) {
	k, err := c.keyFor(purpose)
	if err != nil {
		return nil, err
	}
	return Decrypt(k, ciphertext, []byte(aad))
}
