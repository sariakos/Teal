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

// Platform-wide shared env vars. Admin only on the server.
export const sharedEnvVarsApi = {
	list: (reveal = false) =>
		api.get<EnvVarRow[]>(`/shared-envvars${reveal ? '?reveal=true' : ''}`),
	upsert: (key: string, value: string) =>
		api.post<void>('/shared-envvars', { key, value }),
	remove: (key: string) =>
		api.delete<void>(`/shared-envvars/${encodeURIComponent(key)}`)
};
