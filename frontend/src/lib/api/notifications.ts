import { api } from './client';

export type NotificationLevel = 'info' | 'warn' | 'error';

export interface NotificationRow {
	id: number;
	level: NotificationLevel;
	kind: string;
	title: string;
	body?: string;
	appSlug?: string;
	createdAt: string;
	readAt?: string | null;
	userScope: 'you' | 'broadcast';
}

export interface NotificationsListResponse {
	items: NotificationRow[];
	unread: number;
}

export const notificationsApi = {
	list: (limit = 50) => api.get<NotificationsListResponse>(`/notifications?limit=${limit}`),
	markRead: (id: number) => api.post<void>(`/notifications/${id}/read`),
	markAllRead: () => api.post<void>('/notifications/read-all')
};

export interface PlatformSummary {
	appCount: number;
	runningContainers: number;
	totalDiskBytes: number;
	workdirDiskBytes: number;
	recentFailures: Array<{
		appSlug: string;
		deploymentId: number;
		failureReason: string;
		completedAt: string;
	}>;
}

export const platformApi = {
	summary: () => api.get<PlatformSummary>('/platform/summary'),
	selfUpdate: () =>
		api.post<{ status: string; marker: string; message: string }>(
			'/platform/self-update?confirm=update-platform'
		)
};
