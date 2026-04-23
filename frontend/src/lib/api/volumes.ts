import { api } from './client';
import type { DockerVolume } from './types';

export const volumesApi = {
	// Optional appSlug filters server-side to volumes whose name starts with
	// the app's compose project prefix (<slug>-blue_ / <slug>-green_).
	list: (appSlug?: string) =>
		api.get<DockerVolume[]>(`/docker/volumes${appSlug ? `?app=${encodeURIComponent(appSlug)}` : ''}`),
	// remove requires confirm == name to make a misclick impossible.
	remove: (name: string) =>
		api.delete<void>(`/docker/volumes/${encodeURIComponent(name)}?confirm=${encodeURIComponent(name)}`)
};
