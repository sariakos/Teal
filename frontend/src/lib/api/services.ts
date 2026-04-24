import { api } from './client';

export interface ServiceInfo {
	name: string;
	image?: string;
	hasBuild: boolean;
	exposedPorts?: number[];
}

// ServicesResponse — `source` is "checkout" (parsed from the latest
// successful deploy's clone), "stored" (from App.composeFile), or
// "none" (the app hasn't been deployed and has no stored compose yet).
// `hint` accompanies "none" with a user-facing explanation.
export interface ServicesResponse {
	services: ServiceInfo[];
	source: 'checkout' | 'stored' | 'none';
	hint?: string;
}

export const servicesApi = {
	list: (slug: string) => api.get<ServicesResponse>(`/apps/${slug}/services`)
};
