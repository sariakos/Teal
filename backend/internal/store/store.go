// Package store owns Teal's persistence layer. It opens the SQLite database,
// runs schema migrations on startup, and exposes one repository per
// aggregate. Callers receive domain types and pass domain types — SQL stays
// inside this package.
//
// What it does:
//   - Owns the *sql.DB lifecycle (Open, Close).
//   - Runs embedded migrations on Open so the database schema always matches
//     the binary version.
//   - Provides repositories: AppRepo, DeploymentRepo, UserRepo, EnvVarRepo,
//     AuditLogRepo. Each has a small, explicit interface.
//
// What it does NOT do:
//   - Cache. SQLite is fast enough for our load; an in-process cache would
//     just be a stale-read source. Add caching only after profiling shows a
//     bottleneck, never before.
//   - Validate domain invariants beyond what the schema enforces. Higher
//     layers own business rules.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// ErrNotFound is returned by repository Get/GetBy* methods when no row
// matches. Callers should compare with errors.Is, not by value, so future
// wrappers don't break.
var ErrNotFound = errors.New("store: not found")

// Store bundles the database handle and every repository. Construct one with
// Open and pass it down to whoever needs persistence.
type Store struct {
	DB *sql.DB

	Apps             *AppRepo
	Deployments      *DeploymentRepo
	Users            *UserRepo
	EnvVars          *EnvVarRepo
	AppSharedEnvVars *AppSharedEnvVarRepo
	PlatformSettings *PlatformSettingsRepo
	Metrics          *MetricsRepo
	Notifications    *NotificationsRepo
	AuditLogs        *AuditLogRepo
	Sessions         *SessionRepo
	APIKeys          *APIKeyRepo
}

// Open creates the parent directory of dbPath if needed, opens the SQLite
// database with WAL + foreign-keys + busy-timeout configured, applies any
// pending migrations, and returns a ready Store.
//
// The special path ":memory:" is honoured for tests and skips directory
// creation.
func Open(ctx context.Context, dbPath string) (*Store, error) {
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, fmt.Errorf("store: create db dir: %w", err)
		}
	}

	dsn := buildDSN(dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// SQLite is single-writer. Cap connections to avoid lock contention noise
	// in logs and let the busy_timeout do its job. Reads still use the same
	// pool — modernc's driver serialises writes internally.
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return &Store{
		DB:               db,
		Apps:             &AppRepo{db: db},
		Deployments:      &DeploymentRepo{db: db},
		Users:            &UserRepo{db: db},
		EnvVars:          &EnvVarRepo{db: db},
		AppSharedEnvVars: &AppSharedEnvVarRepo{db: db},
		PlatformSettings: &PlatformSettingsRepo{db: db},
		Metrics:          &MetricsRepo{db: db},
		Notifications:    &NotificationsRepo{db: db},
		AuditLogs:        &AuditLogRepo{db: db},
		Sessions:         &SessionRepo{db: db},
		APIKeys:          &APIKeyRepo{db: db},
	}, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// buildDSN composes a modernc.org/sqlite DSN with PRAGMA settings encoded as
// query parameters. The driver applies these on every new connection, which
// is important because per-connection PRAGMAs (foreign_keys, busy_timeout)
// don't carry across reconnects.
func buildDSN(path string) string {
	q := url.Values{}
	q.Set("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)") // safe with WAL; faster than FULL
	return path + "?" + q.Encode()
}
