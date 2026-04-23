import { api } from './client';
import type { User, UserRole } from './types';

export const usersApi = {
	list: () => api.get<User[]>('/users'),
	create: (data: { email: string; password: string; role: UserRole }) =>
		api.post<User>('/users', data),
	update: (id: number, data: Partial<{ email: string; password: string; role: UserRole }>) =>
		api.patch<User>(`/users/${id}`, data),
	delete: (id: number) => api.delete<void>(`/users/${id}`)
};
