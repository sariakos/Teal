import { api, ApiError } from './client';
import type { MeResponse } from './types';

export interface SetupStatus {
	noUsersYet: boolean;
}

// fetchMe returns the current user, or null when unauthenticated.
export async function fetchMe(): Promise<MeResponse | null> {
	try {
		return await api.get<MeResponse>('/me');
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) return null;
		throw err;
	}
}

export async function fetchSetupStatus(): Promise<SetupStatus> {
	return api.get<SetupStatus>('/setup-status');
}

export async function login(email: string, password: string): Promise<MeResponse> {
	return api.post<MeResponse>('/login', { email, password });
}

export async function logout(): Promise<void> {
	await api.post<void>('/logout');
}

export async function bootstrapAdmin(email: string, password: string): Promise<MeResponse> {
	return api.post<MeResponse>('/register-bootstrap', { email, password });
}
