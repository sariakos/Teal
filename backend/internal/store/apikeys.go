package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// APIKeyRepo persists domain.APIKey. The raw key is never persisted — the
// caller hashes (SHA-256) before passing in KeyHash.
type APIKeyRepo struct {
	db *sql.DB
}

// Create inserts a new APIKey. CreatedAt is stamped here; LastUsedAt and
// RevokedAt remain nil at creation.
func (r *APIKeyRepo) Create(ctx context.Context, k domain.APIKey) (domain.APIKey, error) {
	k.CreatedAt = time.Now().UTC()

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO api_keys (user_id, name, key_hash, last_used_at, revoked_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		k.UserID, k.Name, k.KeyHash,
		formatNullableTime(k.LastUsedAt), formatNullableTime(k.RevokedAt),
		formatTime(k.CreatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.APIKey{}, ErrConflict
		}
		return domain.APIKey{}, fmt.Errorf("api_keys: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.APIKey{}, err
	}
	k.ID = id
	return k, nil
}

// Get returns an APIKey by ID, or ErrNotFound.
func (r *APIKeyRepo) Get(ctx context.Context, id int64) (domain.APIKey, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE id = ?`, id)
	return scanAPIKey(row)
}

// GetByHash looks up an APIKey by its SHA-256 hash. The auth path uses this
// to authenticate a presented bearer token. Returns ErrNotFound for unknown
// or revoked keys (the auth layer treats both as "no key").
func (r *APIKeyRepo) GetByHash(ctx context.Context, hash []byte) (domain.APIKey, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE key_hash = ? AND revoked_at IS NULL`, hash)
	return scanAPIKey(row)
}

// ListForUser returns all keys (including revoked) for a User, newest first.
// Useful for the settings UI.
func (r *APIKeyRepo) ListForUser(ctx context.Context, userID int64) ([]domain.APIKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE user_id = ? ORDER BY created_at DESC, id DESC`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("api_keys: list for user: %w", err)
	}
	defer rows.Close()

	var out []domain.APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// MarkUsed bumps last_used_at without round-tripping the row.
func (r *APIKeyRepo) MarkUsed(ctx context.Context, id int64, at time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		formatTime(at), id)
	if err != nil {
		return fmt.Errorf("api_keys: mark used: %w", err)
	}
	return nil
}

// Revoke marks the key as revoked. Subsequent GetByHash calls will not
// return it. Returns ErrNotFound if the row doesn't exist.
func (r *APIKeyRepo) Revoke(ctx context.Context, id int64, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		formatTime(at), id)
	if err != nil {
		return fmt.Errorf("api_keys: revoke: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

const apiKeyColumns = `id, user_id, name, key_hash, last_used_at, revoked_at, created_at`

func scanAPIKey(s scanner) (domain.APIKey, error) {
	var (
		k                                domain.APIKey
		lastUsed, revokedAt              sql.NullString
		createdAt                        string
	)
	err := s.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &lastUsed, &revokedAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.APIKey{}, ErrNotFound
		}
		return domain.APIKey{}, fmt.Errorf("api_keys: scan: %w", err)
	}
	if lastUsed.Valid {
		s := lastUsed.String
		t, err := parseNullableTime(&s)
		if err != nil {
			return domain.APIKey{}, err
		}
		k.LastUsedAt = t
	}
	if revokedAt.Valid {
		s := revokedAt.String
		t, err := parseNullableTime(&s)
		if err != nil {
			return domain.APIKey{}, err
		}
		k.RevokedAt = t
	}
	if k.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.APIKey{}, err
	}
	return k, nil
}
