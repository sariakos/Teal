package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migration is one parsed migration file.
type migration struct {
	version int    // numeric prefix, e.g. 0001 -> 1
	name    string // full filename, e.g. "0001_initial_schema.sql"
	sql     string // file contents
}

// Migrate applies any migrations that have not yet been recorded in
// schema_migrations, in version order. It is safe to call on every startup —
// already-applied migrations are skipped.
//
// The contract:
//   - Migrations are loaded from the embedded migrations/ directory.
//   - Filenames must match "NNNN_<description>.sql" where NNNN is a
//     non-negative integer. Versions must be unique.
//   - Each file is applied inside its own transaction. If any statement
//     fails the transaction rolls back and Migrate returns the error; the
//     database is left at the previous version.
//   - schema_migrations is created if it doesn't exist before any user
//     migration runs.
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	applied, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return fmt.Errorf("load applied versions: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
	}
	return nil
}

func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	const ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`
	_, err := db.ExecContext(ctx, ddl)
	return err
}

func loadAppliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}

	var ms []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if other, dup := seen[v]; dup {
			return nil, fmt.Errorf("duplicate migration version %d in %q and %q", v, other, e.Name())
		}
		seen[v] = e.Name()

		body, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{version: v, name: e.Name(), sql: string(body)})
	}

	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return ms, nil
}

// parseVersion extracts the integer prefix from "NNNN_anything.sql".
func parseVersion(filename string) (int, error) {
	idx := strings.IndexByte(filename, '_')
	if idx <= 0 {
		return 0, fmt.Errorf("missing version prefix (expected NNNN_<name>.sql)")
	}
	v, err := strconv.Atoi(filename[:idx])
	if err != nil {
		return 0, fmt.Errorf("version prefix not an integer: %w", err)
	}
	if v < 0 {
		return 0, fmt.Errorf("version must be non-negative")
	}
	return v, nil
}

func applyOne(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op if committed

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`,
		m.version, m.name,
	); err != nil {
		return err
	}
	return tx.Commit()
}
