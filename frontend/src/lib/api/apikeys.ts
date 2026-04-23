import { api } from './client';
import type { ApiKey, ApiKeyCreateResponse } from './types';

export const apiKeysApi = {
	list: () => api.get<ApiKey[]>('/apikeys'),
	create: (name: string) => api.post<ApiKeyCreateResponse>('/apikeys', { name }),
	revoke: (id: number) => api.delete<void>(`/apikeys/${id}`)
};
