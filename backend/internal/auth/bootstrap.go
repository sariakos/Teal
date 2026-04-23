package auth

import (
	"context"
	"database/sql"
)

// NoUsersYet reports whether the users table is empty. The /setup endpoint
// in the API uses this to decide whether unauthenticated bootstrap is
// allowed: until the first admin exists, anyone reaching the platform on
// localhost is by definition the operator. Once at least one user exists,
// the endpoint refuses.
//
// This lives in auth (not store) because the policy ("first user creation
// is exempt from auth") is an authentication concern. The query itself is
// trivial enough that going through a repo would just add indirection.
func NoUsersYet(ctx context.Context, db *sql.DB) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users LIMIT 1`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}
