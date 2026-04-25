<script lang="ts">
	/*
	 * App-detail Logs tab. Lists active containers in a selector and
	 * renders LogStream for the chosen container. On selection change:
	 *   1. Fetch persisted history (HTTP)
	 *   2. Mount LogStream pointed at the live topic, seeded with history
	 */
	import { onMount } from 'svelte';
	import { logsApi, type ContainerSummary, type LogLine } from '$lib/api/logs';
	import LogStream from './LogStream.svelte';
	import Select from './Select.svelte';
	import EmptyState from './EmptyState.svelte';
	import { ScrollText, RefreshCw } from '@lucide/svelte';

	let { slug }: { slug: string } = $props();

	let containers = $state<ContainerSummary[]>([]);
	let selected = $state<string>('');
	let history = $state<LogLine[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	async function reload() {
		try {
			containers = await logsApi.listContainers(slug);
			error = null;
			if (containers.length > 0 && !selected) {
				selected = containers[0].id;
				await loadHistory(selected);
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load containers';
		} finally {
			loading = false;
		}
	}

	async function loadHistory(id: string) {
		try {
			history = await logsApi.containerLogs(id, '1h', 1000);
		} catch {
			history = [];
		}
	}

	$effect(() => {
		if (selected) {
			void loadHistory(selected);
		}
	});

	onMount(reload);
</script>

<div class="space-y-3">
	{#if loading}
		<div class="text-sm text-[var(--color-fg-muted)]">Loading containers…</div>
	{:else if error}
		<div class="text-sm text-[var(--color-danger)]">{error}</div>
	{:else if containers.length === 0}
		<EmptyState
			icon={ScrollText}
			title="No containers yet"
			description="Logs show up here once the first deploy is running."
		/>
	{:else}
		<div class="flex items-center gap-3 text-sm">
			<label for="containerpick" class="shrink-0 text-[var(--color-fg-muted)]">Container</label>
			<div class="flex-1 max-w-xs">
				<Select id="containerpick" size="sm" bind:value={selected}>
					{#each containers as c}
						<option value={c.id}>{c.name} ({c.color})</option>
					{/each}
				</Select>
			</div>
			<button
				class="inline-flex items-center gap-1 text-xs text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
				onclick={reload}
			>
				<RefreshCw class="h-3 w-3" />
				Refresh
			</button>
		</div>
		{#key selected}
			<LogStream topic={`containerlogs.${selected}`} seed={history} />
		{/key}
	{/if}
</div>
