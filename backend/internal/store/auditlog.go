package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// AuditLogRepo persists domain.AuditLog. The repository is intentionally
// append-only — there is no Update or Delete. Audit history must remain
// immutable; if a row is wrong, the fix is to record a corrective action,
// not to rewrite the past.
type AuditLogRepo struct {
	db *sql.DB
}

// Append inserts a new audit log row. CreatedAt is stamped here.
func (r *AuditLogRepo) Append(ctx context.Context, l domain.AuditLog) (domain.AuditLog, error) {
	l.CreatedAt = time.Now().UTC()

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO audit_logs (actor_user_id, actor, action, target_type, target_id, ip, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableInt64(l.ActorUserID), l.Actor, string(l.Action), l.TargetType, l.TargetID,
		l.IP, l.Details, formatTime(l.CreatedAt),
	)
	if err != nil {
		return domain.AuditLog{}, fmt.Errorf("audit_logs: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.AuditLog{}, err
	}
	l.ID = id
	return l, nil
}

// List returns audit log rows newest first. limit > 0 caps the result.
// Filtering by action/target/actor will be added when the audit UI lands;
// keep this method simple until a real consumer needs more.
func (r *AuditLogRepo) List(ctx context.Context, limit int) ([]domain.AuditLog, error) {
	q := `SELECT ` + auditColumns + ` FROM audit_logs ORDER BY created_at DESC, id DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("audit_logs: list: %w", err)
	}
	defer rows.Close()

	var out []domain.AuditLog
	for rows.Next() {
		l, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

const auditColumns = `id, actor_user_id, actor, action, target_type, target_id, ip, details, created_at`

func scanAuditLog(s scanner) (domain.AuditLog, error) {
	var (
		l           domain.AuditLog
		actorUserID sql.NullInt64
		actionStr   string
		createdAt   string
	)
	err := s.Scan(&l.ID, &actorUserID, &l.Actor, &actionStr, &l.TargetType, &l.TargetID,
		&l.IP, &l.Details, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuditLog{}, ErrNotFound
		}
		return domain.AuditLog{}, fmt.Errorf("audit_logs: scan: %w", err)
	}
	if actorUserID.Valid {
		v := actorUserID.Int64
		l.ActorUserID = &v
	}
	l.Action = domain.AuditAction(actionStr)
	if l.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.AuditLog{}, err
	}
	return l, nil
}
