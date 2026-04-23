package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// NotificationsRepo persists domain.Notification rows. Notifications
// are user-facing (read state, scoped, prunable) — distinct from the
// audit log (forensic, immutable, all events).
type NotificationsRepo struct {
	db *sql.DB
}

// Insert writes a new notification. CreatedAt is stamped here.
func (r *NotificationsRepo) Insert(ctx context.Context, n domain.Notification) (domain.Notification, error) {
	n.CreatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (user_id, level, kind, title, body, app_slug, created_at, read_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableInt64(n.UserID), string(n.Level), string(n.Kind),
		n.Title, n.Body, n.AppSlug,
		formatTime(n.CreatedAt), formatNullableTime(n.ReadAt),
	)
	if err != nil {
		return domain.Notification{}, fmt.Errorf("notifications: insert: %w", err)
	}
	id, _ := res.LastInsertId()
	n.ID = id
	return n, nil
}

// ListForUser returns the most recent notifications visible to userID:
// user-targeted rows AND broadcast rows (user_id IS NULL) when the user
// is an admin. Caller is responsible for the admin check; we accept an
// `includeBroadcasts` flag rather than re-querying users.
func (r *NotificationsRepo) ListForUser(ctx context.Context, userID int64, includeBroadcasts bool, limit int) ([]domain.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT ` + notificationColumns + ` FROM notifications
	      WHERE user_id = ?`
	args := []any{userID}
	if includeBroadcasts {
		q += ` OR user_id IS NULL`
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("notifications: list: %w", err)
	}
	defer rows.Close()
	return scanNotifications(rows)
}

// CountUnreadForUser is a cheap aggregate the UI bell calls. Same
// scoping as ListForUser.
func (r *NotificationsRepo) CountUnreadForUser(ctx context.Context, userID int64, includeBroadcasts bool) (int, error) {
	q := `SELECT COUNT(*) FROM notifications WHERE read_at IS NULL AND (user_id = ?`
	args := []any{userID}
	if includeBroadcasts {
		q += ` OR user_id IS NULL`
	}
	q += `)`
	var n int
	if err := r.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// MarkRead flips a single notification's read_at. Returns ErrNotFound
// if no row matched (typo or different user trying to mark someone
// else's row).
func (r *NotificationsRepo) MarkRead(ctx context.Context, id, userID int64, includeBroadcasts bool) error {
	q := `UPDATE notifications SET read_at = ?
	      WHERE id = ? AND read_at IS NULL AND (user_id = ?`
	args := []any{formatTime(time.Now().UTC()), id, userID}
	if includeBroadcasts {
		q += ` OR user_id IS NULL`
	}
	q += `)`
	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("notifications: mark read: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAllReadForUser clears the unread badge in one shot.
func (r *NotificationsRepo) MarkAllReadForUser(ctx context.Context, userID int64, includeBroadcasts bool) error {
	q := `UPDATE notifications SET read_at = ?
	      WHERE read_at IS NULL AND (user_id = ?`
	args := []any{formatTime(time.Now().UTC()), userID}
	if includeBroadcasts {
		q += ` OR user_id IS NULL`
	}
	q += `)`
	_, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("notifications: mark all read: %w", err)
	}
	return nil
}

// Prune drops notifications older than `before`. Called by a periodic
// goroutine (or could be inline; the table is small).
func (r *NotificationsRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM notifications WHERE created_at < ?`, formatTime(before))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const notificationColumns = `id, user_id, level, kind, title, body, app_slug, created_at, read_at`

func scanNotifications(rows *sql.Rows) ([]domain.Notification, error) {
	var out []domain.Notification
	for rows.Next() {
		var (
			n           domain.Notification
			userID      sql.NullInt64
			levelStr    string
			kindStr     string
			created     string
			readAt      sql.NullString
		)
		if err := rows.Scan(&n.ID, &userID, &levelStr, &kindStr,
			&n.Title, &n.Body, &n.AppSlug, &created, &readAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		n.Level = domain.NotificationLevel(levelStr)
		n.Kind = domain.NotificationKind(kindStr)
		if userID.Valid {
			v := userID.Int64
			n.UserID = &v
		}
		if t, err := parseTime(created); err == nil {
			n.CreatedAt = t
		}
		if readAt.Valid {
			s := readAt.String
			t, err := parseNullableTime(&s)
			if err != nil {
				return nil, err
			}
			n.ReadAt = t
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
