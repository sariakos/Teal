package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// UserRepo persists domain.User. The package never logs PasswordHash or the
// TOTP secret; callers should follow the same rule.
type UserRepo struct {
	db *sql.DB
}

// Create inserts a new User. Email uniqueness is enforced by the schema; a
// duplicate returns the underlying constraint error wrapped in fmt.Errorf —
// callers can inspect with errors.Is against driver-specific sentinels if
// needed, but more typically they should validate uniqueness via GetByEmail
// first to surface a clean error to the user.
func (r *UserRepo) Create(ctx context.Context, u domain.User) (domain.User, error) {
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	if u.Role == "" {
		u.Role = domain.UserRoleViewer
	}

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO users (email, password_hash, role, totp_secret_encrypted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		u.Email, u.PasswordHash, string(u.Role), notNilBytes(u.TOTPSecretEncrypted),
		formatTime(u.CreatedAt), formatTime(u.UpdatedAt),
	)
	if err != nil {
		if e := translateInsertError(err); errors.Is(e, ErrConflict) {
			return domain.User{}, ErrConflict
		}
		return domain.User{}, fmt.Errorf("users: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.User{}, err
	}
	u.ID = id
	return u, nil
}

// Get returns the User with the given ID, or ErrNotFound.
func (r *UserRepo) Get(ctx context.Context, id int64) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetByEmail returns the User with the given email (case-sensitive — emails
// are stored as the user typed them; comparisons should normalise upstream).
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email)
	return scanUser(row)
}

// List returns all users ordered by email.
func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY email ASC`)
	if err != nil {
		return nil, fmt.Errorf("users: list: %w", err)
	}
	defer rows.Close()

	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Update writes back mutable fields. Email is updatable — the unique
// constraint will surface a duplicate.
func (r *UserRepo) Update(ctx context.Context, u domain.User) error {
	u.UpdatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE users SET email = ?, password_hash = ?, role = ?, totp_secret_encrypted = ?, updated_at = ?
		WHERE id = ?`,
		u.Email, u.PasswordHash, string(u.Role), notNilBytes(u.TOTPSecretEncrypted),
		formatTime(u.UpdatedAt), u.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("users: update: %w", err)
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

// Delete removes a User. Deployments triggered by this user keep their rows
// but have triggered_by_user_id NULLed (FK ON DELETE SET NULL). Audit logs
// behave the same. We prefer this over CASCADE so historical records are not
// silently lost when an operator removes someone.
func (r *UserRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("users: delete: %w", err)
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

const userColumns = `id, email, password_hash, role, totp_secret_encrypted, created_at, updated_at`

func scanUser(s scanner) (domain.User, error) {
	var (
		u                  domain.User
		roleStr            string
		createdAt, updated string
	)
	err := s.Scan(&u.ID, &u.Email, &u.PasswordHash, &roleStr, &u.TOTPSecretEncrypted, &createdAt, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, fmt.Errorf("users: scan: %w", err)
	}
	u.Role = domain.UserRole(roleStr)
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.User{}, err
	}
	if u.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.User{}, err
	}
	return u, nil
}
