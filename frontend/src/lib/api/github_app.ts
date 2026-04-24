import { api } from './client';

export interface GitHubAppConfig {
	appId: number;
	appSlug: string;
	hasPrivateKey: boolean;
	hasWebhookSecret: boolean;
}

export interface GitHubAppUpdate {
	appId?: number;
	appSlug?: string;
	privateKeyPem?: string;
	webhookSecret?: string;
}

export const githubAppApi = {
	get: () => api.get<GitHubAppConfig>('/settings/github-app'),
	put: (data: GitHubAppUpdate) => api.put<void>('/settings/github-app', data)
};
