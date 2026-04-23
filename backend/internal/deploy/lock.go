package deploy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// ErrLocked is returned by Lock.Acquire when another deployment is already
// in flight for the same App. The HTTP handler translates this to 409.
var ErrLocked = errors.New("deploy: another deployment is already in progress")

// Lock guards against concurrent deploys for the same App. The lock IS the
// deployment row: a deployment in 'pending' or 'running' status holds it.
// Acquire creates a new pending row inside a SQLite IMMEDIATE transaction so
// the check-and-insert is atomic; Done updates the row to a terminal state.
type Lock struct {
	db *sql.DB
}

// NewLock constructs a Lock bound to db.
func NewLock(db *sql.DB) *Lock {
	return &Lock{db: db}
}

// AcquireParams describes the new deployment row to insert.
type AcquireParams struct {
	AppID             int64
	Color             domain.Color
	CommitSHA         string
	TriggeredByUserID *int64
	EnvVarSetHash     string
	TriggerKind       domain.TriggerKind
}

// Acquire inserts a new deployment in 'pending' status. Returns ErrLocked if
// any non-terminal deployment already exists for the App.
//
// Why a transaction: SQLite's BEGIN IMMEDIATE acquires a reserved lock
// immediately, preventing two concurrent callers from both seeing zero
// in-flight deployments and each inserting one.
func (l *Lock) Acquire(ctx context.Context, p AcquireParams) (domain.Deployment, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Deployment{}, fmt.Errorf("lock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	var inFlight int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM deployments
		WHERE app_id = ? AND status IN ('pending','running')`, p.AppID,
	).Scan(&inFlight)
	if err != nil {
		return domain.Deployment{}, fmt.Errorf("lock: check in-flight: %w", err)
	}
	if inFlight > 0 {
		return domain.Deployment{}, ErrLocked
	}

	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO deployments (app_id, color, status, commit_sha, triggered_by_user_id, env_var_set_hash, trigger_kind, created_at)
		VALUES (?, ?, 'pending', ?, ?, ?, ?, ?)`,
		p.AppID, string(p.Color), p.CommitSHA, nullableInt64(p.TriggeredByUserID), p.EnvVarSetHash, string(p.TriggerKind), formatTime(now),
	)
	if err != nil {
		return domain.Deployment{}, fmt.Errorf("lock: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Deployment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Deployment{}, fmt.Errorf("lock: commit: %w", err)
	}
	return domain.Deployment{
		ID:                id,
		AppID:             p.AppID,
		Color:             p.Color,
		Status:            domain.DeploymentStatusPending,
		CommitSHA:         p.CommitSHA,
		TriggeredByUserID: p.TriggeredByUserID,
		EnvVarSetHash:     p.EnvVarSetHash,
		TriggerKind:       p.TriggerKind,
		CreatedAt:         now,
	}, nil
}

// Started marks an acquired deployment as running and stamps started_at.
func (l *Lock) Started(ctx context.Context, deploymentID int64) error {
	now := time.Now().UTC()
	_, err := l.db.ExecContext(ctx,
		`UPDATE deployments SET status = 'running', started_at = ? WHERE id = ?`,
		formatTime(now), deploymentID,
	)
	if err != nil {
		return fmt.Errorf("lock: set started: %w", err)
	}
	return nil
}

// Done writes a terminal status onto the deployment row, releasing the
// lock. failureReason is set only for non-success terminal states.
func (l *Lock) Done(ctx context.Context, deploymentID int64, status domain.DeploymentStatus, failureReason string) error {
	now := time.Now().UTC()
	_, err := l.db.ExecContext(ctx,
		`UPDATE deployments SET status = ?, completed_at = ?, failure_reason = ? WHERE id = ?`,
		string(status), formatTime(now), failureReason, deploymentID,
	)
	if err != nil {
		return fmt.Errorf("lock: set done: %w", err)
	}
	return nil
}

// formatTime mirrors store.formatTime; kept private to deploy so we don't
// expand the store package's exported surface for this internal need.
func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
