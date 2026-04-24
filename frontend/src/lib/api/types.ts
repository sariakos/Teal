// Mirrors the backend's wire shapes (see internal/api/*.go). Keep field
// names and casing identical — divergence here would force per-page
// translation everywhere.

export type UserRole = 'admin' | 'member' | 'viewer';

export interface User {
	id: number;
	email: string;
	role: UserRole;
	hasTotp: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface MeResponse {
	user: User;
	csrfToken?: string;
}

export type AppStatus = 'idle' | 'deploying' | 'running' | 'failed' | 'stopped';
export type Color = 'blue' | 'green' | '';
export type GitAuthKind = '' | 'ssh' | 'pat' | 'github_app';

export interface App {
	id: number;
	slug: string;
	name: string;
	domains: string[];
	activeColor: Color;
	autoDeployBranch: string;
	autoDeployEnabled: boolean;
	status: AppStatus;
	lastDeployedCommitSha?: string;
	createdAt: string;
	updatedAt: string;
}

export interface AppDetail extends App {
	composeFile: string;
	gitUrl?: string;
	gitAuthKind?: GitAuthKind;
	gitBranch?: string;
	gitComposePath?: string;
	hasGitCredential: boolean;
	hasWebhookSecret: boolean;
	cpuLimit?: string;
	memoryLimit?: string;
	notificationWebhookUrl?: string;
	hasNotificationSecret?: boolean;
	notificationEmail?: string;
	githubAppInstallationId?: number;
	githubAppRepo?: string;
}

// One-shot response from PATCH /apps/{slug} when git/webhook secrets are
// freshly generated. The two `new*` fields are ONLY present on the turn
// that generated them and must be surfaced to the user immediately.
export interface AppInitSecrets extends AppDetail {
	newWebhookSecret?: string;
	newPublicKey?: string;
	newKeyFingerprint?: string;
}

export interface DeployKey {
	publicKey: string;
	fingerprint: string;
}

export type DeploymentStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';

// Phase mirrors deploy.Phase in the backend. Empty when the engine has no
// in-memory state for the deployment (terminal: read Status instead).
export type DeploymentPhase =
	| 'pending'
	| 'pulling'
	| 'building'
	| 'starting'
	| 'healthcheck'
	| 'flipping_traffic'
	| 'draining'
	| 'tearing_down'
	| 'succeeded'
	| 'failed'
	| '';

export interface Deployment {
	id: number;
	appId: number;
	color: 'blue' | 'green';
	status: DeploymentStatus;
	phase?: DeploymentPhase;
	commitSha: string;
	triggeredByUserId?: number;
	startedAt?: string;
	completedAt?: string;
	failureReason?: string;
	createdAt: string;
}

export interface ApiKey {
	id: number;
	name: string;
	lastUsedAt?: string;
	revokedAt?: string;
	createdAt: string;
}

export interface ApiKeyCreateResponse extends ApiKey {
	key: string; // raw — shown once
}

export interface AuditLog {
	id: number;
	actorUserId?: number;
	actor: string;
	action: string;
	targetType?: string;
	targetId?: string;
	ip?: string;
	details?: string;
	createdAt: string;
}

// Phase 5 — env vars + platform settings + volumes.

export interface EnvVarRow {
	key: string;
	value?: string; // present only when ?reveal=true was requested (audited)
	masked: boolean;
	hasValue: boolean;
	updatedAt: string;
}

export interface AppSharedListing {
	included: string[];
	available: string[];
}

export interface PlatformSetting {
	key: string;
	value: string;
	updatedAt: string;
}

export interface PlatformSettingMutation {
	restartTraefik: boolean;
}

export interface DockerVolume {
	name: string;
	driver: string;
	mountpoint: string;
	createdAt: string;
	labels?: Record<string, string>;
}
