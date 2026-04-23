package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// SessionRepo persists domain.Session.
type SessionRepo struct {
	db *sql.DB
}

// Create inserts a new Session. CreatedAt and LastSeenAt are stamped to now;
// the caller is responsible for filling ExpiresAt and CSRFToken (the auth
// package owns those policy choices).
func (r *SessionRepo) Create(ctx context.Context, s domain.Session) (domain.Session, error) {
	now := time.Now().UTC()
	s.CreatedAt = now
	s.LastSeenAt = now

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, csrf_token, ip, user_agent, expires_at, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.CSRFToken, s.IP, s.UserAgent,
		formatTime(s.ExpiresAt), formatTime(s.LastSeenAt), formatTime(s.CreatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Session{}, ErrConflict
		}
		return domain.Session{}, fmt.Errorf("sessions: insert: %w", err)
	}
	return s, nil
}

// Get returns the Session with the given ID, or ErrNotFound. Expired
// sessions are returned as-is — the auth layer enforces expiry policy.
func (r *SessionRepo) Get(ctx context.Context, id string) (domain.Session, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	return scanSession(row)
}

// Touch advances last_seen_at and (if extending) expires_at without round-
// tripping the entire row. Returns ErrNotFound if the session was deleted
// between read and update.
//
// The auth middleware should call this only when meaningfully extending
// (e.g. > 1 minute since LastSeenAt) to avoid a write per request.
func (r *SessionRepo) Touch(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
		formatTime(lastSeenAt), formatTime(expiresAt), id,
	)
	if err != nil {
		return fmt.Errorf("sessions: touch: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete revokes a single Session by ID. Returns nil even when no row
// matched — logout should be idempotent.
func (r *SessionRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sessions: delete: %w", err)
	}
	return nil
}

// DeleteForUser revokes every Session for a User (used on password change or
// admin-initiated revoke).
func (r *SessionRepo) DeleteForUser(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("sessions: delete for user: %w", err)
	}
	return nil
}

// DeleteExpired removes sessions whose expires_at is at or before `at`.
// Returns the number of rows removed. Run periodically by a sweeper.
func (r *SessionRepo) DeleteExpired(ctx context.Context, at time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(at))
	if err != nil {
		return 0, fmt.Errorf("sessions: delete expired: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const sessionColumns = `id, user_id, csrf_token, ip, user_agent, expires_at, last_seen_at, created_at`

func scanSession(s scanner) (domain.Session, error) {
	var (
		out                                                         domain.Session
		expiresAt, lastSeenAt, createdAt                            string
	)
	err := s.Scan(&out.ID, &out.UserID, &out.CSRFToken, &out.IP, &out.UserAgent,
		&expiresAt, &lastSeenAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Session{}, ErrNotFound
		}
		return domain.Session{}, fmt.Errorf("sessions: scan: %w", err)
	}
	if out.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return domain.Session{}, err
	}
	if out.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return domain.Session{}, err
	}
	if out.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Session{}, err
	}
	return out, nil
}
