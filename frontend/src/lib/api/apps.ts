import { api } from './client';
import type {
	App,
	AppDetail,
	AppInitSecrets,
	DeployKey,
	Deployment,
	GitAuthKind,
	Route
} from './types';

export interface CreateAppInput {
	slug: string;
	name: string;
	composeFile: string;
	domains: string[];
	autoDeployBranch?: string;
	autoDeployEnabled?: boolean;

	// Git source (optional). When `gitUrl` is set, the create response
	// is an AppInitSecrets that may carry one-shot reveals
	// (newPublicKey for SSH, newWebhookSecret for the inbound webhook).
	gitUrl?: string;
	gitAuthKind?: GitAuthKind;
	gitCredential?: string;
	gitBranch?: string;
	gitComposePath?: string;
	routes?: Route[];
}

export interface UpdateAppInput {
	name?: string;
	composeFile?: string;
	domains?: string[];
	autoDeployBranch?: string;
	autoDeployEnabled?: boolean;
	gitUrl?: string;
	gitAuthKind?: GitAuthKind;
	gitCredential?: string;
	gitBranch?: string;
	gitComposePath?: string;
	cpuLimit?: string;
	memoryLimit?: string;
	notificationWebhookUrl?: string;
	notificationEmail?: string;
	githubAppInstallationId?: number;
	githubAppRepo?: string;
	routes?: Route[];
}

export const appsApi = {
	list: () => api.get<App[]>('/apps'),
	get: (slug: string) => api.get<AppDetail>(`/apps/${slug}`),
	create: (data: CreateAppInput) => api.post<AppInitSecrets>('/apps', data),
	// update returns the one-shot secrets shape — callers should check
	// newWebhookSecret / newPublicKey and surface them immediately.
	update: (slug: string, data: UpdateAppInput) => api.patch<AppInitSecrets>(`/apps/${slug}`, data),
	delete: (slug: string) => api.delete<void>(`/apps/${slug}`),
	deploy: (slug: string, commitSha?: string) =>
		api.post<Deployment>(`/apps/${slug}/deploy`, commitSha ? { commitSha } : {}),
	rollback: (slug: string) => api.post<Deployment>(`/apps/${slug}/rollback`, {}),
	deployments: (slug: string) => api.get<Deployment[]>(`/apps/${slug}/deployments`),
	getDeployKey: (slug: string) => api.get<DeployKey>(`/apps/${slug}/deploy-key`),
	rotateDeployKey: (slug: string) => api.post<DeployKey>(`/apps/${slug}/rotate-deploy-key`, {}),
	rotateWebhookSecret: (slug: string) =>
		api.post<{ webhookSecret: string }>(`/apps/${slug}/rotate-webhook-secret`, {}),
	rotateNotificationSecret: (slug: string) =>
		api.post<{ webhookSecret: string }>(`/apps/${slug}/rotate-notification-secret`, {}),
	startGitHubAppInstall: (slug: string) =>
		api.post<{ installUrl: string; callbackUrl: string }>(`/apps/${slug}/install-github-app`, {})
};
