package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// openMemDB opens a fresh in-memory SQLite for a single test. Each test gets
// its own DB so they can run in any order.
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	// Use a unique DSN per test so connections share the same in-memory DB
	// (the default ":memory:" DSN gives each connection its own DB, which
	// breaks WAL/multi-conn assumptions).
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateAppliesAllMigrationsOnce(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	// schema_migrations should record exactly the migrations on disk.
	expected, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&got); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if got != len(expected) {
		t.Errorf("schema_migrations count = %d, want %d", got, len(expected))
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second: %v", err)
	}

	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&got); err != nil {
		t.Fatalf("count: %v", err)
	}
	expected, _ := loadMigrations()
	if got != len(expected) {
		t.Errorf("idempotent re-run changed count: got %d, want %d", got, len(expected))
	}
}

func TestMigrateCreatesCoreTables(t *testing.T) {
	db := openMemDB(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{"apps", "deployments", "users", "env_vars", "audit_logs", "schema_migrations"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"0001_initial.sql", 1, false},
		{"42_thing.sql", 42, false},
		{"no_prefix.sql", 0, true},
		{"_underscore.sql", 0, true},
		{"abc_thing.sql", 0, true},
	}
	for _, tc := range cases {
		got, err := parseVersion(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseVersion(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseVersion(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseVersion(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
