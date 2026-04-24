package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// AppRepo persists domain.App. Methods accept a context for cancellation and
// translate driver errors into store sentinels (ErrNotFound) where useful.
type AppRepo struct {
	db *sql.DB
}

// Create inserts a new App. CreatedAt and UpdatedAt are stamped here; any
// values set on the input are ignored. The new ID is written back onto the
// returned struct.
func (r *AppRepo) Create(ctx context.Context, a domain.App) (domain.App, error) {
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.Status == "" {
		a.Status = domain.AppStatusIdle
	}

	if a.GitComposePath == "" {
		a.GitComposePath = "docker-compose.yml"
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO apps (slug, name, compose_file, auto_deploy_branch, auto_deploy_enabled,
		                  domains, active_color, queue_deploys, status,
		                  git_url, git_auth_kind, git_auth_credential_encrypted,
		                  git_branch, git_compose_path, webhook_secret_encrypted,
		                  last_deployed_commit_sha,
		                  cpu_limit, memory_limit,
		                  notification_webhook_url, notification_webhook_secret_encrypted, notification_email,
		                  github_app_installation_id, github_app_repo,
		                  created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Slug, a.Name, a.ComposeFile, a.AutoDeployBranch, boolToInt(a.AutoDeployEnabled),
		a.Domains, string(a.ActiveColor), boolToInt(a.QueueDeploys),
		string(a.Status),
		a.GitURL, string(a.GitAuthKind), notNilBytes(a.GitAuthCredentialEncrypted),
		a.GitBranch, a.GitComposePath, notNilBytes(a.WebhookSecretEncrypted),
		a.LastDeployedCommitSHA,
		a.CPULimit, a.MemoryLimit,
		a.NotificationWebhookURL, notNilBytes(a.NotificationWebhookSecretEncrypted), a.NotificationEmail,
		a.GitHubAppInstallationID, a.GitHubAppRepo,
		formatTime(a.CreatedAt), formatTime(a.UpdatedAt),
	)
	if err != nil {
		if e := translateInsertError(err); errors.Is(e, ErrConflict) {
			return domain.App{}, ErrConflict
		}
		return domain.App{}, fmt.Errorf("apps: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.App{}, err
	}
	a.ID = id
	return a, nil
}

// Get returns the App with the given ID, or ErrNotFound.
func (r *AppRepo) Get(ctx context.Context, id int64) (domain.App, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE id = ?`, id)
	return scanApp(row)
}

// GetBySlug returns the App with the given slug, or ErrNotFound.
func (r *AppRepo) GetBySlug(ctx context.Context, slug string) (domain.App, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE slug = ?`, slug)
	return scanApp(row)
}

// List returns all Apps ordered by name. Phase 1 doesn't paginate — the UI
// dashboard shows them all on one page; we'll add pagination if/when an
// instance has hundreds of Apps.
func (r *AppRepo) List(ctx context.Context) ([]domain.App, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+appColumns+` FROM apps ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("apps: list: %w", err)
	}
	defer rows.Close()

	var out []domain.App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Update writes mutable fields back. Slug is not updatable (it's part of the
// Compose project name and changing it would orphan running Stacks); the
// caller must delete and recreate to change a slug.
func (r *AppRepo) Update(ctx context.Context, a domain.App) error {
	a.UpdatedAt = time.Now().UTC()
	if a.GitComposePath == "" {
		a.GitComposePath = "docker-compose.yml"
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE apps SET name = ?, compose_file = ?, auto_deploy_branch = ?, auto_deploy_enabled = ?,
		                domains = ?, active_color = ?, queue_deploys = ?, status = ?,
		                git_url = ?, git_auth_kind = ?, git_auth_credential_encrypted = ?,
		                git_branch = ?, git_compose_path = ?, webhook_secret_encrypted = ?,
		                last_deployed_commit_sha = ?,
		                cpu_limit = ?, memory_limit = ?,
		                notification_webhook_url = ?, notification_webhook_secret_encrypted = ?, notification_email = ?,
		                github_app_installation_id = ?, github_app_repo = ?,
		                updated_at = ?
		WHERE id = ?`,
		a.Name, a.ComposeFile, a.AutoDeployBranch, boolToInt(a.AutoDeployEnabled),
		a.Domains, string(a.ActiveColor), boolToInt(a.QueueDeploys),
		string(a.Status),
		a.GitURL, string(a.GitAuthKind), notNilBytes(a.GitAuthCredentialEncrypted),
		a.GitBranch, a.GitComposePath, notNilBytes(a.WebhookSecretEncrypted),
		a.LastDeployedCommitSHA,
		a.CPULimit, a.MemoryLimit,
		a.NotificationWebhookURL, notNilBytes(a.NotificationWebhookSecretEncrypted), a.NotificationEmail,
		a.GitHubAppInstallationID, a.GitHubAppRepo,
		formatTime(a.UpdatedAt), a.ID,
	)
	if err != nil {
		return fmt.Errorf("apps: update: %w", err)
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

// SetLastDeployedCommitSHA updates the denormalised last-deployed SHA. Called
// by the engine after a successful deploy so the dashboard list can render
// the commit without an N+1 join into deployments.
func (r *AppRepo) SetLastDeployedCommitSHA(ctx context.Context, id int64, sha string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE apps SET last_deployed_commit_sha = ?, updated_at = ? WHERE id = ?`,
		sha, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("apps: set last deployed sha: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetActiveColor flips the recorded active color and refreshes updated_at.
// Called by the deploy engine after a successful Traefik flip, before the
// old stack is torn down — at that point the new color is the source of
// truth for routing.
func (r *AppRepo) SetActiveColor(ctx context.Context, id int64, color domain.Color) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE apps SET active_color = ?, updated_at = ? WHERE id = ?`,
		string(color), formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("apps: set active color: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetStatus updates the App's coarse status without touching anything else.
// Used by the engine to advance idle → deploying → running/failed without
// stomping on concurrent UI edits to other fields.
func (r *AppRepo) SetStatus(ctx context.Context, id int64, status domain.AppStatus) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE apps SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("apps: set status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the App and (via FK cascade) its env_vars and deployments.
func (r *AppRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("apps: delete: %w", err)
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

const appColumns = `id, slug, name, compose_file, auto_deploy_branch, auto_deploy_enabled,
	domains, active_color, queue_deploys, status,
	git_url, git_auth_kind, git_auth_credential_encrypted, git_branch, git_compose_path,
	webhook_secret_encrypted, last_deployed_commit_sha,
	cpu_limit, memory_limit,
	notification_webhook_url, notification_webhook_secret_encrypted, notification_email,
	github_app_installation_id, github_app_repo,
	created_at, updated_at`

// scanner is the minimal interface implemented by both *sql.Row and *sql.Rows
// so scanApp/scanUser/etc can be reused for single-row and multi-row reads.
type scanner interface {
	Scan(dest ...any) error
}

func scanApp(s scanner) (domain.App, error) {
	var (
		a                                domain.App
		autoEnabled, queueDep            int
		statusStr, activeColor, authKind string
		createdAt, updated               string
	)
	err := s.Scan(&a.ID, &a.Slug, &a.Name, &a.ComposeFile, &a.AutoDeployBranch,
		&autoEnabled, &a.Domains, &activeColor, &queueDep, &statusStr,
		&a.GitURL, &authKind, &a.GitAuthCredentialEncrypted,
		&a.GitBranch, &a.GitComposePath, &a.WebhookSecretEncrypted,
		&a.LastDeployedCommitSHA,
		&a.CPULimit, &a.MemoryLimit,
		&a.NotificationWebhookURL, &a.NotificationWebhookSecretEncrypted, &a.NotificationEmail,
		&a.GitHubAppInstallationID, &a.GitHubAppRepo,
		&createdAt, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.App{}, ErrNotFound
		}
		return domain.App{}, fmt.Errorf("apps: scan: %w", err)
	}
	a.AutoDeployEnabled = autoEnabled != 0
	a.QueueDeploys = queueDep != 0
	a.ActiveColor = domain.Color(activeColor)
	a.GitAuthKind = domain.GitAuthKind(authKind)
	a.Status = domain.AppStatus(statusStr)
	if a.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.App{}, err
	}
	if a.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.App{}, err
	}
	return a, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
