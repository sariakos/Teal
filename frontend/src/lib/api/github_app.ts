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

// One-click manifest-create flow. The init call returns the manifest
// JSON + the GitHub URL the browser must POST it to (with state for
// CSRF). The frontend builds an auto-submitted hidden form so the
// browser navigates to GitHub. After the user clicks Create, GitHub
// redirects back to /api/v1/settings/github-app/manifest-callback,
// which exchanges the temporary code, persists the App's credentials,
// and 303s back to /settings/github-app?created=<slug>.
//
// Manifest type loosely mirrors the Go-side struct — frontend only
// needs to round-trip it as a form field, so a Record<string, unknown>
// is enough type safety.
export interface ManifestInitResponse {
	manifest: Record<string, unknown>;
	postUrl: string;
	state: string;
}

export const githubAppApi = {
	get: () => api.get<GitHubAppConfig>('/settings/github-app'),
	put: (data: GitHubAppUpdate) => api.put<void>('/settings/github-app', data),
	manifestInit: (org?: string) =>
		api.post<ManifestInitResponse>('/settings/github-app/manifest-init', {
			org: org ?? ''
		})
};

// Per-app repo picker. Returns one entry per installation of the
// platform App, each carrying the repos that installation can see.
// `configured: false` means the platform App hasn't been set up yet —
// UI links the operator to /settings/github-app.
export interface RepoEntry {
	fullName: string;
	private: boolean;
	defaultBranch: string;
}

export interface InstallationEntry {
	installationId: number;
	accountLogin: string;
	accountType: string;
	repos: RepoEntry[];
}

export interface AppReposResponse {
	configured: boolean;
	appSlug?: string;
	installations: InstallationEntry[];
}

export const githubAppReposApi = {
	list: (slug: string) => api.get<AppReposResponse>(`/apps/${slug}/github-app/repos`),
	// Used by the new-app form, before any app exists.
	listGlobal: () => api.get<AppReposResponse>('/github-app/repos')
};
