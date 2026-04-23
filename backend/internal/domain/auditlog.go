package domain

import "time"

// AuditAction is a short, stable identifier for a recorded action. New
// actions are added as new constants; existing values must never be reused
// for a different meaning, since old log entries reference them.
type AuditAction string

const (
	AuditActionUserLogin       AuditAction = "user.login"
	AuditActionUserLogout      AuditAction = "user.logout"
	AuditActionUserCreate      AuditAction = "user.create"
	AuditActionUserUpdate      AuditAction = "user.update"
	AuditActionUserDelete      AuditAction = "user.delete"
	AuditActionAppCreate       AuditAction = "app.create"
	AuditActionAppUpdate       AuditAction = "app.update"
	AuditActionAppDelete       AuditAction = "app.delete"
	AuditActionDeploymentStart    AuditAction = "deployment.start"
	AuditActionDeploymentRollback AuditAction = "deployment.rollback"
	AuditActionEnvVarUpdate       AuditAction = "envvar.update"
	AuditActionEnvVarDelete       AuditAction = "envvar.delete"
	AuditActionEnvVarReveal       AuditAction = "envvar.reveal"
	AuditActionSharedEnvVarUpdate AuditAction = "shared_envvar.update"
	AuditActionSharedEnvVarDelete AuditAction = "shared_envvar.delete"
	AuditActionAppSharedEnvSet    AuditAction = "app_shared_envvar.set"
	AuditActionPlatformSettingSet AuditAction = "platform_setting.set"
	AuditActionVolumeDelete       AuditAction = "volume.delete"
)

// AuditLog is an immutable record of a state-changing action. Rows are
// append-only; the schema enforces this at the repository layer (no Update,
// no Delete methods).
//
// Privacy:
//   - IP is stored as a string (IPv4 or IPv6, possibly with port stripped).
//   - Details is a free-form string that should NOT contain secrets. The
//     repository does not inspect it; callers are responsible.
type AuditLog struct {
	ID int64

	// ActorUserID is the user responsible for the action. Nil when the action
	// originated from a non-user actor (e.g. a webhook); in that case Actor
	// describes the source.
	ActorUserID *int64
	Actor       string // human-readable actor description, always set

	Action     AuditAction
	TargetType string // e.g. "app", "user", "deployment"
	TargetID   string // free-form ID of the target ("" if action is global)

	IP      string
	Details string

	CreatedAt time.Time
}
