import { api } from './client';
import type { Deployment } from './types';

export const deploymentsApi = {
	get: (id: number) => api.get<Deployment>(`/deployments/${id}`),
	list: (limit?: number) => api.get<Deployment[]>(`/deployments${limit ? `?limit=${limit}` : ''}`)
};
