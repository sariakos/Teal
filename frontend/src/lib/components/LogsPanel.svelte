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
		<div class="text-sm text-zinc-500">Loading containers…</div>
	{:else if error}
		<div class="text-sm text-red-600">{error}</div>
	{:else if containers.length === 0}
		<div class="text-sm text-zinc-500">
			No platform-managed containers running for this app yet. Deploy first.
		</div>
	{:else}
		<div class="flex items-center gap-2 text-sm">
			<label for="containerpick" class="text-zinc-500">Container:</label>
			<select
				id="containerpick"
				bind:value={selected}
				class="rounded-md border border-zinc-300 bg-white px-2 py-1 text-sm"
			>
				{#each containers as c}
					<option value={c.id}>{c.name} ({c.color})</option>
				{/each}
			</select>
			<button class="ml-2 text-xs text-teal-700 hover:underline" onclick={reload}>
				Refresh container list
			</button>
		</div>
		{#key selected}
			<LogStream topic={`containerlogs.${selected}`} seed={history} />
		{/key}
	{/if}
</div>
