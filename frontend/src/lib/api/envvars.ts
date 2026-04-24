import { api } from './client';
import type { AppSharedListing, EnvVarRow } from './types';

// Per-app env vars. The `reveal` form returns plaintext and is audited
// server-side — call it sparingly.
export const appEnvVarsApi = {
	list: (slug: string, reveal = false) =>
		api.get<EnvVarRow[]>(`/apps/${slug}/envvars${reveal ? '?reveal=true' : ''}`),
	upsert: (slug: string, key: string, value: string) =>
		api.post<void>(`/apps/${slug}/envvars`, { key, value }),
	remove: (slug: string, key: string) =>
		api.delete<void>(`/apps/${slug}/envvars/${encodeURIComponent(key)}`)
};

// Per-app shared-envvar allow-list. The `available` field on listing tells
// the UI which platform-wide shared keys exist; `included` is what this app
// has opted in to.
export const appSharedEnvVarsApi = {
	list: (slug: string) => api.get<AppSharedListing>(`/apps/${slug}/shared-envvars`),
	set: (slug: string, keys: string[]) =>
		api.put<void>(`/apps/${slug}/shared-envvars`, { keys })
};

// Required env vars discovered from the app's compose project.
//
// status:
//   set       — per-app value present, will be passed to compose at deploy
//   shared    — app opted into a shared var with this key
//   default   — unset but compose has ${VAR:-default}, will use that
//   missing   — unset and no default → deploy will likely fail
//   unclaimed — a shared var with this key exists but the app hasn't opted in
export type RequiredEnvVarStatus = 'set' | 'shared' | 'default' | 'missing' | 'unclaimed';

export interface RequiredEnvVar {
	name: string;
	status: RequiredEnvVarStatus;
	hasDefault: boolean;
	defaultValue?: string;
	sources: string[];
}

export interface RequiredEnvVarsResponse {
	vars: RequiredEnvVar[];
	source: 'checkout' | 'stored' | 'none';
	hint?: string;
}

export const requiredEnvVarsApi = {
	list: (slug: string) => api.get<RequiredEnvVarsResponse>(`/apps/${slug}/required-envvars`)
};

// Platform-wide shared env vars. Admin only on the server.
export const sharedEnvVarsApi = {
	list: (reveal = false) =>
		api.get<EnvVarRow[]>(`/shared-envvars${reveal ? '?reveal=true' : ''}`),
	upsert: (key: string, value: string) =>
		api.post<void>('/shared-envvars', { key, value }),
	remove: (key: string) =>
		api.delete<void>(`/shared-envvars/${encodeURIComponent(key)}`)
};
