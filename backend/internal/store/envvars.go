package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// EnvVarRepo persists domain.EnvVar. Values are passed through as bytes —
// encryption/decryption is the auth/secrets package's job, not this one.
type EnvVarRepo struct {
	db *sql.DB
}

// Create inserts a new EnvVar. Scope/AppID consistency is enforced by the
// schema CHECK; an attempt to violate it returns the underlying constraint
// error.
func (r *EnvVarRepo) Create(ctx context.Context, e domain.EnvVar) (domain.EnvVar, error) {
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO env_vars (scope, app_id, key, value_encrypted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		string(e.Scope), nullableInt64(e.AppID), e.Key, e.ValueEncrypted,
		formatTime(e.CreatedAt), formatTime(e.UpdatedAt),
	)
	if err != nil {
		if errs := translateInsertError(err); errors.Is(errs, ErrConflict) {
			return domain.EnvVar{}, ErrConflict
		}
		return domain.EnvVar{}, fmt.Errorf("env_vars: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.EnvVar{}, err
	}
	e.ID = id
	return e, nil
}

// Get returns the EnvVar by ID, or ErrNotFound.
func (r *EnvVarRepo) Get(ctx context.Context, id int64) (domain.EnvVar, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+envVarColumns+` FROM env_vars WHERE id = ?`, id)
	return scanEnvVar(row)
}

// ListForApp returns all env vars belonging to an App, ordered by key.
func (r *EnvVarRepo) ListForApp(ctx context.Context, appID int64) ([]domain.EnvVar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+envVarColumns+` FROM env_vars WHERE scope = 'app' AND app_id = ? ORDER BY key ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("env_vars: list for app: %w", err)
	}
	defer rows.Close()
	return scanEnvVars(rows)
}

// ListShared returns all shared env vars, ordered by key.
func (r *EnvVarRepo) ListShared(ctx context.Context) ([]domain.EnvVar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+envVarColumns+` FROM env_vars WHERE scope = 'shared' ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("env_vars: list shared: %w", err)
	}
	defer rows.Close()
	return scanEnvVars(rows)
}

// Update replaces the value of an existing EnvVar.
func (r *EnvVarRepo) Update(ctx context.Context, e domain.EnvVar) error {
	e.UpdatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE env_vars SET value_encrypted = ?, updated_at = ? WHERE id = ?`,
		e.ValueEncrypted, formatTime(e.UpdatedAt), e.ID,
	)
	if err != nil {
		return fmt.Errorf("env_vars: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Upsert inserts or updates an app-scoped env var by (AppID, Key). Returns
// the persisted row. Shared-scope upserts go through UpsertShared because
// they have different uniqueness semantics (global key).
func (r *EnvVarRepo) Upsert(ctx context.Context, appID int64, key string, ciphertext []byte) (domain.EnvVar, error) {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO env_vars (scope, app_id, key, value_encrypted, created_at, updated_at)
		VALUES ('app', ?, ?, ?, ?, ?)
		ON CONFLICT(app_id, key) WHERE scope = 'app' DO UPDATE
		    SET value_encrypted = excluded.value_encrypted, updated_at = excluded.updated_at`,
		appID, key, ciphertext, formatTime(now), formatTime(now))
	if err != nil {
		return domain.EnvVar{}, fmt.Errorf("env_vars: upsert app: %w", err)
	}
	return r.GetByAppAndKey(ctx, appID, key)
}

// UpsertShared inserts or updates a shared env var by Key.
func (r *EnvVarRepo) UpsertShared(ctx context.Context, key string, ciphertext []byte) (domain.EnvVar, error) {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO env_vars (scope, app_id, key, value_encrypted, created_at, updated_at)
		VALUES ('shared', NULL, ?, ?, ?, ?)
		ON CONFLICT(key) WHERE scope = 'shared' DO UPDATE
		    SET value_encrypted = excluded.value_encrypted, updated_at = excluded.updated_at`,
		key, ciphertext, formatTime(now), formatTime(now))
	if err != nil {
		return domain.EnvVar{}, fmt.Errorf("env_vars: upsert shared: %w", err)
	}
	return r.GetShared(ctx, key)
}

// GetByAppAndKey fetches an app-scoped env var by natural key.
func (r *EnvVarRepo) GetByAppAndKey(ctx context.Context, appID int64, key string) (domain.EnvVar, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+envVarColumns+` FROM env_vars WHERE scope = 'app' AND app_id = ? AND key = ?`,
		appID, key)
	return scanEnvVar(row)
}

// GetShared fetches a shared env var by key.
func (r *EnvVarRepo) GetShared(ctx context.Context, key string) (domain.EnvVar, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+envVarColumns+` FROM env_vars WHERE scope = 'shared' AND key = ?`, key)
	return scanEnvVar(row)
}

// DeleteByAppAndKey removes an app-scoped env var by natural key. Returns
// ErrNotFound if no row matched.
func (r *EnvVarRepo) DeleteByAppAndKey(ctx context.Context, appID int64, key string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM env_vars WHERE scope = 'app' AND app_id = ? AND key = ?`, appID, key)
	if err != nil {
		return fmt.Errorf("env_vars: delete app: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteShared removes a shared env var by key. Returns ErrNotFound if no
// row matched. The app allow-lists pointing at this key are left intact;
// the engine logs a warning when an opted-in key has no matching shared
// row.
func (r *EnvVarRepo) DeleteShared(ctx context.Context, key string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM env_vars WHERE scope = 'shared' AND key = ?`, key)
	if err != nil {
		return fmt.Errorf("env_vars: delete shared: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes an EnvVar by ID.
func (r *EnvVarRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM env_vars WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("env_vars: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

const envVarColumns = `id, scope, app_id, key, value_encrypted, created_at, updated_at`

func scanEnvVar(s scanner) (domain.EnvVar, error) {
	var (
		e                  domain.EnvVar
		scopeStr           string
		appID              sql.NullInt64
		createdAt, updated string
	)
	err := s.Scan(&e.ID, &scopeStr, &appID, &e.Key, &e.ValueEncrypted, &createdAt, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EnvVar{}, ErrNotFound
		}
		return domain.EnvVar{}, fmt.Errorf("env_vars: scan: %w", err)
	}
	e.Scope = domain.EnvVarScope(scopeStr)
	if appID.Valid {
		v := appID.Int64
		e.AppID = &v
	}
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.EnvVar{}, err
	}
	if e.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.EnvVar{}, err
	}
	return e, nil
}

func scanEnvVars(rows *sql.Rows) ([]domain.EnvVar, error) {
	var out []domain.EnvVar
	for rows.Next() {
		e, err := scanEnvVar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
