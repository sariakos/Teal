package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// APIKeyPrefix marks Teal-issued tokens so they're recognisable in logs and
// secret scanners. The full format is "tk_" + base32(32 random bytes), with
// padding stripped.
const APIKeyPrefix = "tk_"

// ErrInvalidAPIKey is returned when a presented bearer token is malformed,
// unknown, or revoked. Callers should treat all three as the same outcome
// to avoid leaking which case applied.
var ErrInvalidAPIKey = errors.New("auth: invalid api key")

// APIKeyManager owns API-key generation and validation. One instance shared
// across all requests; safe for concurrent use.
type APIKeyManager struct {
	APIKeys *store.APIKeyRepo
}

// NewAPIKeyManager constructs a manager.
func NewAPIKeyManager(repo *store.APIKeyRepo) *APIKeyManager {
	return &APIKeyManager{APIKeys: repo}
}

// Generate creates a new key for userID with the given human-readable name.
// It returns the raw key (which the caller MUST display to the user exactly
// once and then forget) and the persisted APIKey row. Only the SHA-256 of
// the raw key is stored.
func (m *APIKeyManager) Generate(ctx context.Context, userID int64, name string) (raw string, key domain.APIKey, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", domain.APIKey{}, err
	}
	raw = APIKeyPrefix + strings.TrimRight(base32.StdEncoding.EncodeToString(b[:]), "=")
	hash := HashKey(raw)

	saved, err := m.APIKeys.Create(ctx, domain.APIKey{
		UserID:  userID,
		Name:    name,
		KeyHash: hash,
	})
	if err != nil {
		return "", domain.APIKey{}, err
	}
	return raw, saved, nil
}

// Validate looks up the key behind a presented raw token and bumps its
// last_used_at. Returns ErrInvalidAPIKey on any failure.
func (m *APIKeyManager) Validate(ctx context.Context, raw string) (domain.APIKey, error) {
	if !strings.HasPrefix(raw, APIKeyPrefix) {
		return domain.APIKey{}, ErrInvalidAPIKey
	}
	hash := HashKey(raw)
	key, err := m.APIKeys.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.APIKey{}, ErrInvalidAPIKey
		}
		return domain.APIKey{}, err
	}
	// MarkUsed is best-effort; failing to update last_used_at must not
	// reject an otherwise-valid key.
	_ = m.APIKeys.MarkUsed(ctx, key.ID, time.Now().UTC())
	return key, nil
}

// HashKey computes the SHA-256 of a raw API key. Exported so tests and the
// installer can compute the same value the manager would.
func HashKey(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// ConstantTimeEqualHash compares two hashes in constant time. Used by tests;
// production validation goes through GetByHash which is itself a constant-
// time DB lookup on a unique index.
func ConstantTimeEqualHash(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// ParseBearer extracts the raw token from "Authorization: Bearer <token>".
// Returns ("", false) if the header is missing or not a Bearer scheme.
func ParseBearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}
