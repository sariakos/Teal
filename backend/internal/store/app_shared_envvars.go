package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AppSharedEnvVarRepo tracks which shared env-var keys each App has opted
// in to. Shared vars are NOT universal — each App names the keys it wants
// and only those are injected.
//
// We intentionally do NOT enforce a foreign key to env_vars(key). The
// shared row can be deleted and re-created; the allow-list survives. The
// engine treats an allow-listed key with no matching shared row as a no-op
// with a warning in the deploy log.
type AppSharedEnvVarRepo struct {
	db *sql.DB
}

// Set overwrites the allow-list for appID with exactly keys. Duplicates
// and empty strings are ignored; order is irrelevant. Runs in a single
// transaction so the set transitions atomically.
func (r *AppSharedEnvVarRepo) Set(ctx context.Context, appID int64, keys []string) error {
	seen := make(map[string]struct{}, len(keys))
	normalized := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		normalized = append(normalized, k)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM app_shared_env_vars WHERE app_id = ?`, appID); err != nil {
		return fmt.Errorf("app_shared_env_vars: clear: %w", err)
	}
	if len(normalized) > 0 {
		now := formatTime(time.Now().UTC())
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO app_shared_env_vars (app_id, key, created_at) VALUES (?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("app_shared_env_vars: prepare: %w", err)
		}
		defer stmt.Close()
		for _, k := range normalized {
			if _, err := stmt.ExecContext(ctx, appID, k, now); err != nil {
				return fmt.Errorf("app_shared_env_vars: insert: %w", err)
			}
		}
	}
	return tx.Commit()
}

// ListForApp returns the keys the App has opted in to, ordered by key.
func (r *AppSharedEnvVarRepo) ListForApp(ctx context.Context, appID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key FROM app_shared_env_vars WHERE app_id = ? ORDER BY key ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("app_shared_env_vars: list: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
