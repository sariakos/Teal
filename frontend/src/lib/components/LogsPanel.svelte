<script lang="ts">
	/*
	 * App-detail Logs tab. Shows a unified stream of every container
	 * belonging to the app, with each line tagged by service name and
	 * color. The container list is itself the multi-select filter:
	 * unchecking a chip hides that container's lines without tearing
	 * down the subscription. Default = all checked.
	 */
	import { onMount } from 'svelte';
	import { logsApi, type ContainerSummary, type LogLine } from '$lib/api/logs';
	import LogStream, { type LineRecord } from './LogStream.svelte';
	import EmptyState from './EmptyState.svelte';
	import { ScrollText, RefreshCw } from '@lucide/svelte';

	let { slug }: { slug: string } = $props();

	let containers = $state<ContainerSummary[]>([]);
	let selected = $state<Set<string>>(new Set());
	let history = $state<LineRecord[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	function labelFor(c: ContainerSummary): string {
		// Service label includes the color in parentheses so blue/green
		// stacks with the same service name don't collide visually.
		const svc = c.service || c.name.replace(/^\//, '');
		return `${svc} (${c.color})`;
	}

	function toneFor(c: ContainerSummary): 'blue' | 'green' | 'neutral' {
		return c.color === 'blue' ? 'blue' : c.color === 'green' ? 'green' : 'neutral';
	}

	const topics = $derived(
		containers.map((c) => ({
			topic: `containerlogs.${c.id}`,
			source: { label: labelFor(c), tone: toneFor(c) }
		}))
	);

	const filterLabels = $derived(
		Array.from(selected)
			.map((id) => containers.find((c) => c.id === id))
			.filter((c): c is ContainerSummary => !!c)
			.map(labelFor)
	);

	async function reload() {
		loading = true;
		try {
			containers = await logsApi.listContainers(slug);
			error = null;
			// On first load (or when the set changed), select everything.
			if (selected.size === 0 || !Array.from(selected).every((id) => containers.some((c) => c.id === id))) {
				selected = new Set(containers.map((c) => c.id));
			}
			await loadHistory();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load containers';
		} finally {
			loading = false;
		}
	}

	async function loadHistory() {
		// Fetch every container's last hour in parallel, tag lines with
		// their source, then merge by timestamp so the unified buffer is
		// chronological from the start.
		try {
			const results = await Promise.all(
				containers.map(async (c) => {
					try {
						const lines = await logsApi.containerLogs(c.id, '1h', 1000);
						const tag = { label: labelFor(c), tone: toneFor(c) };
						return lines.map((l: LogLine) => ({
							t: l.t,
							s: l.s,
							l: l.l,
							source: tag
						})) as LineRecord[];
					} catch {
						return [] as LineRecord[];
					}
				})
			);
			const merged = results.flat();
			merged.sort((a, b) => (a.t ?? '').localeCompare(b.t ?? ''));
			history = merged.slice(-2000);
		} catch {
			history = [];
		}
	}

	function toggle(id: string) {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selected = next;
	}

	function selectAll() {
		selected = new Set(containers.map((c) => c.id));
	}

	function selectNone() {
		selected = new Set();
	}

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
		<div class="flex flex-wrap items-center gap-2 text-sm">
			<span class="shrink-0 text-[var(--color-fg-muted)]">Containers</span>
			{#each containers as c}
				{@const active = selected.has(c.id)}
				<button
					type="button"
					onclick={() => toggle(c.id)}
					class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs transition-colors {active
						? 'border-[var(--color-accent)] bg-[var(--color-accent-soft)] text-[var(--color-fg)]'
						: 'border-[var(--color-border-strong)] bg-[var(--color-bg)] text-[var(--color-fg-subtle)] hover:text-[var(--color-fg)]'}"
				>
					<span
						class="h-1.5 w-1.5 rounded-full {c.color === 'blue'
							? 'bg-sky-400'
							: c.color === 'green'
								? 'bg-emerald-400'
								: 'bg-zinc-400'}"
					></span>
					{labelFor(c)}
				</button>
			{/each}
			<div class="ml-auto flex items-center gap-2">
				<button
					type="button"
					class="text-xs text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
					onclick={selectAll}
				>
					All
				</button>
				<span class="text-xs text-[var(--color-fg-subtle)]">·</span>
				<button
					type="button"
					class="text-xs text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
					onclick={selectNone}
				>
					None
				</button>
				<button
					class="ml-2 inline-flex items-center gap-1 text-xs text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
					onclick={reload}
				>
					<RefreshCw class="h-3 w-3" />
					Refresh
				</button>
			</div>
		</div>
		<LogStream {topics} seed={history} filterSources={filterLabels} />
	{/if}
</div>
