import { api } from './client';

export interface ContainerSummary {
	id: string;
	name: string;
	image: string;
	color: string;
	service: string;
}

export interface LogLine {
	t: string;
	s?: string;
	l?: string;
}

export interface MetricSample {
	containerId: string;
	containerName: string;
	appSlug: string;
	color: string;
	ts: string;
	cpuPct: number;
	memBytes: number;
	memLimit: number;
	netRx: number;
	netTx: number;
	blkRx: number;
	blkTx: number;
}

export const logsApi = {
	listContainers: (slug: string) => api.get<ContainerSummary[]>(`/apps/${slug}/containers`),
	containerLogs: (id: string, since?: string, limit?: number) => {
		const q = new URLSearchParams();
		if (since) q.set('since', since);
		if (limit) q.set('limit', String(limit));
		const qs = q.toString();
		return api.get<LogLine[]>(`/containers/${id}/logs${qs ? '?' + qs : ''}`);
	},
	deploymentLog: async (slug: string, deploymentId: number): Promise<string> => {
		// Deployment log is plain text; bypass api.get's JSON path.
		const res = await fetch(`/api/v1/apps/${slug}/deployments/${deploymentId}/log`, {
			credentials: 'include'
		});
		if (!res.ok) throw new Error(`deployment log: ${res.status}`);
		return res.text();
	}
};

export const metricsApi = {
	list: (slug: string, since?: string) => {
		const q = since ? `?since=${encodeURIComponent(since)}` : '';
		return api.get<MetricSample[]>(`/apps/${slug}/metrics${q}`);
	}
};
