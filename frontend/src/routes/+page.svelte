<!--
  Dashboard. Lists apps with their status and provides quick deploy access.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { appsApi } from '$lib/api/apps';
	import { platformApi, type PlatformSummary } from '$lib/api/notifications';
	import type { App } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import StatusDot from '$lib/components/StatusDot.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { dirty } from '$lib/stores/dirty.svelte';
	import {
		Boxes,
		Plus,
		ArrowRight,
		Server,
		HardDrive,
		AlertTriangle,
		CheckCircle2,
		Circle
	} from '@lucide/svelte';
	import GithubMark from '$lib/components/GithubMark.svelte';
	import type { Component } from 'svelte';

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
			[apps, summary] = await Promise.all([
				appsApi.list(),
				platformApi.summary().catch(() => null)
			]);
			error = null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load apps';
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger';
	const statusTone: Record<App['status'], Tone> = {
		idle: 'neutral',
		deploying: 'warning',
		running: 'success',
		failed: 'danger',
		stopped: 'neutral'
	};

	const statsList: { label: string; value: () => string; icon: Component }[] = [
		{ label: 'Apps', value: () => String(summary?.appCount ?? 0), icon: Boxes },
		{
			label: 'Running containers',
			value: () => String(summary?.runningContainers ?? 0),
			icon: Server
		},
		{
			label: 'Workdir on disk',
			value: () => fmtBytes(summary?.workdirDiskBytes ?? 0),
			icon: HardDrive
		},
		{
			label: 'Recent failures',
			value: () => String(summary?.recentFailures.length ?? 0),
			icon: AlertTriangle
		}
	];
</script>

<div class="space-y-6">
	<PageHeader
		title="Dashboard"
		description="All apps managed by this Teal instance."
	>
		{#snippet actions()}
			<Button onclick={() => goto('/apps/new')}>
				<Plus class="h-4 w-4" />
				New app
			</Button>
		{/snippet}
	</PageHeader>

	{#if summary && (!summary.githubAppConfigured || summary.appCount === 0)}
		<!-- First-run onboarding card. Disappears once both:
		     - the platform GitHub App is configured (one-click on the
		       Settings page), and
		     - at least one app exists. -->
		<Card title="Get started" description="Two clicks from a fresh install to a deploying app.">
			<ol class="space-y-4 text-sm">
				<li class="flex items-start gap-3">
					{#if summary.githubAppConfigured}
						<CheckCircle2 class="mt-0.5 h-5 w-5 shrink-0 text-[var(--color-success)]" />
					{:else}
						<Circle class="mt-0.5 h-5 w-5 shrink-0 text-[var(--color-fg-subtle)]" />
					{/if}
					<div class="flex-1">
						<div class="flex flex-wrap items-baseline gap-2 font-medium text-[var(--color-fg)]">
							Connect a GitHub App
							{#if summary.githubAppConfigured}
								<Badge tone="success" size="sm">done</Badge>
							{/if}
						</div>
						<p class="mt-0.5 text-xs text-[var(--color-fg-muted)]">
							One click to create + authorise — Teal then auto-fills repo, branch and auth on
							every new app.
						</p>
						{#if !summary.githubAppConfigured}
							<div class="mt-3">
								<Button size="sm" onclick={() => goto('/settings/github-app')}>
									<GithubMark class="h-3.5 w-3.5" />
									Set up the GitHub App
									<ArrowRight class="h-3.5 w-3.5" />
								</Button>
							</div>
						{/if}
					</div>
				</li>
				<li class="flex items-start gap-3">
					{#if summary.appCount > 0}
						<CheckCircle2 class="mt-0.5 h-5 w-5 shrink-0 text-[var(--color-success)]" />
					{:else}
						<Circle class="mt-0.5 h-5 w-5 shrink-0 text-[var(--color-fg-subtle)]" />
					{/if}
					<div class="flex-1">
						<div class="flex flex-wrap items-baseline gap-2 font-medium text-[var(--color-fg)]">
							Create your first app
							{#if summary.appCount > 0}
								<Badge tone="success" size="sm">{summary.appCount} configured</Badge>
							{/if}
						</div>
						<p class="mt-0.5 text-xs text-[var(--color-fg-muted)]">
							Pick a repo from the dropdown — Teal handles the rest.
						</p>
						{#if summary.appCount === 0 && summary.githubAppConfigured}
							<div class="mt-3">
								<Button size="sm" onclick={() => goto('/apps/new')}>
									<Plus class="h-3.5 w-3.5" />
									New app
									<ArrowRight class="h-3.5 w-3.5" />
								</Button>
							</div>
						{/if}
					</div>
				</li>
			</ol>
		</Card>
	{/if}

	{#if summary}
		<div class="grid grid-cols-2 gap-4 md:grid-cols-4">
			{#each statsList as stat}
				{@const Icon = stat.icon}
				<div
					class="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 shadow-[var(--shadow-card)]"
				>
					<div class="flex items-start justify-between">
						<div>
							<div
								class="text-[11px] font-medium uppercase tracking-wide text-[var(--color-fg-subtle)]"
							>
								{stat.label}
							</div>
							<div class="mt-1.5 text-2xl font-semibold text-[var(--color-fg)]">
								{stat.value()}
							</div>
						</div>
						<div
							class="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--color-surface-muted)] text-[var(--color-fg-muted)]"
						>
							<Icon class="h-4 w-4" />
						</div>
					</div>
				</div>
			{/each}
		</div>

	{/if}

	{#if loading}
		<div class="text-sm text-[var(--color-fg-muted)]">Loading apps…</div>
	{:else if error}
		<div class="text-sm text-[var(--color-danger)]">{error}</div>
	{:else if apps.length === 0}
		<EmptyState
			icon={Boxes}
			title="No apps yet"
			description="Add your first docker-compose app to start getting zero-downtime deploys."
		>
			{#snippet action()}
				<Button onclick={() => goto('/apps/new')}>
					<Plus class="h-4 w-4" />
					Create your first app
				</Button>
			{/snippet}
		</EmptyState>
	{:else}
		<Card padded={false}>
			<table class="w-full text-sm">
				<thead
					class="text-left text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
				>
					<tr class="border-b border-[var(--color-border)]">
						<th class="px-5 py-2.5">App</th>
						<th class="px-5 py-2.5">Status</th>
						<th class="px-5 py-2.5">Color</th>
						<th class="px-5 py-2.5">Commit</th>
						<th class="px-5 py-2.5">URLs</th>
						<th class="px-5 py-2.5"></th>
					</tr>
				</thead>
				<tbody>
					{#each apps as app}
						<tr
							class="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-bg-subtle)]"
						>
							<td class="px-5 py-3">
								<div class="flex items-center gap-2">
									<a
										class="font-medium text-[var(--color-fg)] no-underline hover:text-[var(--color-accent)]"
										href={`/apps/${app.slug}`}
									>
										{app.name}
									</a>
									{#if dirty.has(app.slug)}
										<span
											class="h-1.5 w-1.5 rounded-full bg-[var(--color-warning)]"
											title="Configuration changed — redeploy to apply"
										></span>
									{/if}
								</div>
								<div class="mt-0.5 font-mono text-xs text-[var(--color-fg-subtle)]">
									{app.slug}
								</div>
							</td>
							<td class="px-5 py-3">
								<Badge tone={statusTone[app.status]} size="sm">
									<StatusDot tone={statusTone[app.status]} pulse={app.status === 'deploying'} />
									{app.status}
								</Badge>
							</td>
							<td class="px-5 py-3 text-[var(--color-fg-muted)]">{app.activeColor || '—'}</td>
							<td class="px-5 py-3 font-mono text-xs text-[var(--color-fg-muted)]">
								{app.lastDeployedCommitSha ? app.lastDeployedCommitSha.slice(0, 7) : '—'}
							</td>
							<td class="px-5 py-3 text-[var(--color-fg-muted)]">
								{#if (app.routes ?? []).length === 0}
									<span class="text-[var(--color-fg-subtle)]">—</span>
								{:else}
									<div class="flex flex-wrap gap-x-3 gap-y-1">
										{#each app.routes as r}
											<a
												class="text-xs text-[var(--color-accent)] hover:underline"
												href={`https://${r.domain}`}
												target="_blank"
												rel="noopener"
												onclick={(e) => e.stopPropagation()}
											>
												{r.domain}
											</a>
										{/each}
									</div>
								{/if}
							</td>
							<td class="px-5 py-3 text-right">
								<a
									class="inline-flex items-center gap-1 text-xs font-medium text-[var(--color-accent)] hover:underline"
									href={`/apps/${app.slug}`}
								>
									Open
									<ArrowRight class="h-3 w-3" />
								</a>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</Card>
	{/if}

	{#if summary && summary.recentFailures.length > 0}
		<Card title="Recent deploy failures">
			<ul class="divide-y divide-[var(--color-border)] text-sm">
				{#each summary.recentFailures as f}
					<li class="flex items-center justify-between gap-3 py-2.5 first:pt-0 last:pb-0">
						<div class="min-w-0">
							<a
								class="font-medium text-[var(--color-fg)] no-underline hover:text-[var(--color-accent)]"
								href={`/apps/${f.appSlug}`}
							>
								{f.appSlug}
							</a>
							<span class="ml-1.5 font-mono text-xs text-[var(--color-fg-subtle)]">
								#{f.deploymentId}
							</span>
							<div class="mt-0.5 truncate text-xs text-[var(--color-danger)]">
								{f.failureReason || '(no reason recorded)'}
							</div>
						</div>
						<div class="shrink-0 text-xs text-[var(--color-fg-subtle)]">
							{f.completedAt ? new Date(f.completedAt).toLocaleString() : '—'}
						</div>
					</li>
				{/each}
			</ul>
		</Card>
	{/if}
</div>
