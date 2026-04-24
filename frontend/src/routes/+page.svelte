<!--
  Dashboard. Lists apps with their status and provides quick deploy access.
  Phase 3: real data from the backend; clicking an app navigates to its
  detail page where the deploy/rollback controls live.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { appsApi } from '$lib/api/apps';
	import { platformApi, type PlatformSummary } from '$lib/api/notifications';
	import type { App } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';

	let apps = $state<App[]>([]);
	let summary = $state<PlatformSummary | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	function fmtBytes(n: number): string {
		if (n < 1024) return `${n} B`;
		if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
		if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`;
		return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
	}

	async function reload() {
		try {
			[apps, summary] = await Promise.all([appsApi.list(), platformApi.summary().catch(() => null)]);
			error = null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load apps';
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	const statusClass = {
		idle: 'bg-zinc-100 text-zinc-700',
		deploying: 'bg-amber-100 text-amber-800',
		running: 'bg-teal-50 text-teal-700',
		failed: 'bg-red-100 text-red-700',
		stopped: 'bg-zinc-200 text-zinc-700'
	} as const;
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-2xl font-semibold text-zinc-900">Dashboard</h1>
			<p class="mt-1 text-sm text-zinc-500">All apps managed by this Teal instance.</p>
		</div>
		<Button onclick={() => goto('/apps/new')}>New app</Button>
	</div>

	{#if summary}
		<div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
			<Card>
				<div class="text-xs uppercase tracking-wide text-zinc-500">Apps</div>
				<div class="mt-1 text-2xl font-semibold text-zinc-900">{summary.appCount}</div>
			</Card>
			<Card>
				<div class="text-xs uppercase tracking-wide text-zinc-500">Running containers</div>
				<div class="mt-1 text-2xl font-semibold text-zinc-900">{summary.runningContainers}</div>
			</Card>
			<Card>
				<div class="text-xs uppercase tracking-wide text-zinc-500">Workdir on disk</div>
				<div class="mt-1 text-2xl font-semibold text-zinc-900">{fmtBytes(summary.workdirDiskBytes)}</div>
			</Card>
			<Card>
				<div class="text-xs uppercase tracking-wide text-zinc-500">Recent failures</div>
				<div class="mt-1 text-2xl font-semibold text-zinc-900">{summary.recentFailures.length}</div>
			</Card>
		</div>

		{#if summary.recentFailures.length > 0}
			<Card title="Recent deploy failures">
				<ul class="divide-y divide-zinc-100 text-sm">
					{#each summary.recentFailures as f}
						<li class="flex items-center justify-between py-2">
							<div>
								<a class="font-medium text-zinc-800 hover:text-teal-700" href={`/apps/${f.appSlug}`}>
									{f.appSlug}
								</a>
								<span class="ml-2 text-xs text-zinc-500">#{f.deploymentId}</span>
								<div class="text-xs text-red-600">{f.failureReason || '(no reason recorded)'}</div>
							</div>
							<div class="text-xs text-zinc-400">
								{f.completedAt ? new Date(f.completedAt).toLocaleString() : '—'}
							</div>
						</li>
					{/each}
				</ul>
			</Card>
		{/if}
	{/if}

	{#if loading}
		<div class="text-sm text-zinc-500">Loading apps…</div>
	{:else if error}
		<div class="text-sm text-red-600">{error}</div>
	{:else if apps.length === 0}
		<Card title="No apps yet">
			<p class="text-sm text-zinc-600">
				Add your first docker-compose app to start getting zero-downtime deploys.
			</p>
			<div class="mt-4">
				<Button onclick={() => goto('/apps/new')}>Create your first app</Button>
			</div>
		</Card>
	{:else}
		<Card>
			<table class="w-full text-sm">
				<thead class="text-left text-xs uppercase text-zinc-500">
					<tr>
						<th class="pb-2">App</th>
						<th class="pb-2">Status</th>
						<th class="pb-2">Active color</th>
						<th class="pb-2">Commit</th>
						<th class="pb-2">Domains</th>
						<th class="pb-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each apps as app}
						<tr class="border-t border-zinc-100">
							<td class="py-2">
								<a class="font-medium text-zinc-800 hover:text-teal-700" href={`/apps/${app.slug}`}>
									{app.name}
								</a>
								<div class="text-xs text-zinc-400">{app.slug}</div>
							</td>
							<td class="py-2">
								<span class="rounded-full px-2 py-0.5 text-xs {statusClass[app.status]}">
									{app.status}
								</span>
							</td>
							<td class="py-2 text-zinc-600">{app.activeColor || '—'}</td>
							<td class="py-2 font-mono text-xs text-zinc-600">
								{app.lastDeployedCommitSha ? app.lastDeployedCommitSha.slice(0, 7) : '—'}
							</td>
							<td class="py-2 text-zinc-600">
								{#if app.domains.length === 0}
									—
								{:else}
									{#each app.domains as d, i}{#if i > 0}, {/if}<a
											class="text-teal-700 hover:underline"
											href={`https://${d}`}
											target="_blank"
											rel="noopener"
											onclick={(e) => e.stopPropagation()}
										>{d}</a
										>{/each}
								{/if}
							</td>
							<td class="py-2 text-right">
								<a class="text-sm text-teal-700 hover:underline" href={`/apps/${app.slug}`}>
									Open →
								</a>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</Card>
	{/if}
</div>
