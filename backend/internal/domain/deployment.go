package domain

import "time"

// Color marks which of the two parallel Stacks an App is running. The active
// color is whichever Traefik currently routes to; the inactive color is the
// target for the next Deployment.
type Color string

const (
	ColorBlue  Color = "blue"
	ColorGreen Color = "green"
)

// Other returns the opposite color. Used to pick the deploy target.
func (c Color) Other() Color {
	if c == ColorBlue {
		return ColorGreen
	}
	return ColorBlue
}

// TriggerKind records what initiated a Deployment. Empty for rows created
// before Phase 4 (the migration adds the column with a default of '').
type TriggerKind string

const (
	TriggerManual   TriggerKind = "manual"
	TriggerWebhook  TriggerKind = "webhook"
	TriggerRollback TriggerKind = "rollback"
)

// DeploymentStatus tracks a single Deployment through its lifecycle. The
// states are intentionally coarse — fine-grained progress (pulling, building,
// health-checking) is reported via the live event stream, not stored as
// distinct DB states, to keep the schema stable as the engine evolves.
type DeploymentStatus string

const (
	DeploymentStatusPending  DeploymentStatus = "pending"   // queued, not started
	DeploymentStatusRunning  DeploymentStatus = "running"   // actively deploying
	DeploymentStatusSucceeded DeploymentStatus = "succeeded" // completed; this Deployment is now the active Stack
	DeploymentStatusFailed   DeploymentStatus = "failed"    // aborted; the previous Stack remains active
	DeploymentStatusCanceled DeploymentStatus = "canceled"  // user-initiated cancellation
)

// Deployment is one attempt to bring an App to a target version. Stack
// identity ("<app-slug>-<color>") is reconstructed from AppID + Color rather
// than stored separately — see ARCHITECTURE.md "Glossary > Stack" for why
// Stack is not a first-class entity.
type Deployment struct {
	ID    int64
	AppID int64

	Color  Color
	Status DeploymentStatus

	// CommitSHA is the git revision being deployed. Empty for deploys that
	// were not triggered from a git source (e.g. manual recreate of the
	// current Stack).
	CommitSHA string

	// TriggeredByUserID is the user who initiated the deploy. Nil for deploys
	// triggered by webhooks (the webhook itself is the actor; record-keeping
	// goes in AuditLog with the webhook source as the actor).
	TriggeredByUserID *int64

	// EnvVarSetHash records which env-var snapshot was used, without storing
	// the values themselves. Lets us answer "did the env change between these
	// two deploys?" without leaking secrets through the deployment history.
	EnvVarSetHash string

	StartedAt   *time.Time // nil while pending
	CompletedAt *time.Time // nil while pending or running

	// FailureReason is a short, user-facing string set when Status is
	// failed/canceled. The full deploy log is stored separately (Phase 6).
	FailureReason string

	// TriggerKind is what initiated this deployment. Empty for legacy
	// (pre-Phase 4) rows.
	TriggerKind TriggerKind

	CreatedAt time.Time
}
