package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// DeploymentRepo persists domain.Deployment.
type DeploymentRepo struct {
	db *sql.DB
}

// Create inserts a new Deployment in status "pending". CreatedAt is stamped
// here. StartedAt/CompletedAt are nil at creation; the engine sets them via
// Update as the Deployment progresses.
func (r *DeploymentRepo) Create(ctx context.Context, d domain.Deployment) (domain.Deployment, error) {
	d.CreatedAt = time.Now().UTC()
	if d.Status == "" {
		d.Status = domain.DeploymentStatusPending
	}

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO deployments (app_id, color, status, commit_sha, triggered_by_user_id, env_var_set_hash, started_at, completed_at, failure_reason, trigger_kind, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.AppID, string(d.Color), string(d.Status), d.CommitSHA, nullableInt64(d.TriggeredByUserID),
		d.EnvVarSetHash, formatNullableTime(d.StartedAt), formatNullableTime(d.CompletedAt),
		d.FailureReason, string(d.TriggerKind), formatTime(d.CreatedAt),
	)
	if err != nil {
		return domain.Deployment{}, fmt.Errorf("deployments: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Deployment{}, err
	}
	d.ID = id
	return d, nil
}

// Get returns a Deployment by ID, or ErrNotFound.
func (r *DeploymentRepo) Get(ctx context.Context, id int64) (domain.Deployment, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+` FROM deployments WHERE id = ?`, id)
	return scanDeployment(row)
}

// ListForApp returns Deployments for an App, newest first. limit > 0 caps
// the result; pass 0 for "all".
func (r *DeploymentRepo) ListForApp(ctx context.Context, appID int64, limit int) ([]domain.Deployment, error) {
	q := `SELECT ` + deploymentColumns + ` FROM deployments WHERE app_id = ? ORDER BY created_at DESC, id DESC`
	args := []any{appID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("deployments: list: %w", err)
	}
	defer rows.Close()

	var out []domain.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// List returns the most recent Deployments across all Apps. Used by the
// dashboard to show platform-wide activity.
func (r *DeploymentRepo) List(ctx context.Context, limit int) ([]domain.Deployment, error) {
	q := `SELECT ` + deploymentColumns + ` FROM deployments ORDER BY created_at DESC, id DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("deployments: list all: %w", err)
	}
	defer rows.Close()

	var out []domain.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Update writes back fields that change as a Deployment progresses. AppID,
// Color, CommitSHA, and CreatedAt are immutable post-creation.
func (r *DeploymentRepo) Update(ctx context.Context, d domain.Deployment) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE deployments SET status = ?, commit_sha = ?, env_var_set_hash = ?, started_at = ?, completed_at = ?, failure_reason = ?
		WHERE id = ?`,
		string(d.Status), d.CommitSHA, d.EnvVarSetHash, formatNullableTime(d.StartedAt),
		formatNullableTime(d.CompletedAt), d.FailureReason, d.ID,
	)
	if err != nil {
		return fmt.Errorf("deployments: update: %w", err)
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

const deploymentColumns = `id, app_id, color, status, commit_sha, triggered_by_user_id, env_var_set_hash, started_at, completed_at, failure_reason, trigger_kind, created_at`

func scanDeployment(s scanner) (domain.Deployment, error) {
	var (
		d                                domain.Deployment
		colorStr, statusStr, triggerKind string
		triggeredBy                      sql.NullInt64
		startedAt, completedAt           sql.NullString
		createdAt                        string
	)
	err := s.Scan(&d.ID, &d.AppID, &colorStr, &statusStr, &d.CommitSHA,
		&triggeredBy, &d.EnvVarSetHash, &startedAt, &completedAt,
		&d.FailureReason, &triggerKind, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Deployment{}, ErrNotFound
		}
		return domain.Deployment{}, fmt.Errorf("deployments: scan: %w", err)
	}
	d.Color = domain.Color(colorStr)
	d.Status = domain.DeploymentStatus(statusStr)
	d.TriggerKind = domain.TriggerKind(triggerKind)
	if triggeredBy.Valid {
		v := triggeredBy.Int64
		d.TriggeredByUserID = &v
	}
	if startedAt.Valid {
		s := startedAt.String
		t, err := parseNullableTime(&s)
		if err != nil {
			return domain.Deployment{}, err
		}
		d.StartedAt = t
	}
	if completedAt.Valid {
		s := completedAt.String
		t, err := parseNullableTime(&s)
		if err != nil {
			return domain.Deployment{}, err
		}
		d.CompletedAt = t
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Deployment{}, err
	}
	return d, nil
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
