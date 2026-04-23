package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// PlatformSettingsRepo persists domain.PlatformSetting. Keys are the
// primary key; writes upsert.
type PlatformSettingsRepo struct {
	db *sql.DB
}

// Get returns the setting for key, or ErrNotFound. Consumers that want a
// default-on-missing semantic should use GetOrDefault.
func (r *PlatformSettingsRepo) Get(ctx context.Context, key string) (domain.PlatformSetting, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT key, value, updated_at FROM platform_settings WHERE key = ?`, key)

	var s domain.PlatformSetting
	var updated string
	if err := row.Scan(&s.Key, &s.Value, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.PlatformSetting{}, ErrNotFound
		}
		return domain.PlatformSetting{}, fmt.Errorf("platform_settings: scan: %w", err)
	}
	t, err := parseTime(updated)
	if err != nil {
		return domain.PlatformSetting{}, err
	}
	s.UpdatedAt = t
	return s, nil
}

// GetOrDefault returns the string value for key, falling back to def when
// no row exists. Errors other than ErrNotFound still propagate.
func (r *PlatformSettingsRepo) GetOrDefault(ctx context.Context, key, def string) (string, error) {
	s, err := r.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return def, nil
	}
	if err != nil {
		return "", err
	}
	return s.Value, nil
}

// List returns every setting, ordered by key.
func (r *PlatformSettingsRepo) List(ctx context.Context) ([]domain.PlatformSetting, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value, updated_at FROM platform_settings ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("platform_settings: list: %w", err)
	}
	defer rows.Close()

	var out []domain.PlatformSetting
	for rows.Next() {
		var s domain.PlatformSetting
		var updated string
		if err := rows.Scan(&s.Key, &s.Value, &updated); err != nil {
			return nil, err
		}
		t, err := parseTime(updated)
		if err != nil {
			return nil, err
		}
		s.UpdatedAt = t
		out = append(out, s)
	}
	return out, rows.Err()
}

// Set upserts the value for key and stamps UpdatedAt.
func (r *PlatformSettingsRepo) Set(ctx context.Context, key, value string) error {
	now := formatTime(time.Now().UTC())
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO platform_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now)
	if err != nil {
		return fmt.Errorf("platform_settings: upsert: %w", err)
	}
	return nil
}

// Delete removes a setting. Missing rows return nil (idempotent — an admin
// "clearing" a key that was never written should not error).
func (r *PlatformSettingsRepo) Delete(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM platform_settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("platform_settings: delete: %w", err)
	}
	return nil
}
