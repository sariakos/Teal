<script lang="ts">
	/*
	 * Live log viewer. Subscribes to one realtime topic and renders a
	 * scrollable buffer of N lines. Used by:
	 *   - Container logs tab on the App detail page (topic
	 *     "containerlogs.<containerID>")
	 *   - In-flight deploy panel (topic "deploy.<deploymentID>")
	 *
	 * Caller is responsible for any historical-replay HTTP fetch BEFORE
	 * mounting; pass `seed` to seed the buffer with those lines.
	 */
	import { onMount, onDestroy } from 'svelte';
	import { subscribe } from '$lib/realtime/socket';

	interface LineRecord {
		t: string; // ISO-8601
		s?: string; // 'stdout' | 'stderr'
		l?: string; // payload from container logs
		line?: string; // payload from deploy logs
		phase?: string; // phase change events on deploy.<id>
	}

	interface Props {
		topic: string;
		seed?: LineRecord[];
		height?: string; // CSS size, default '24rem'
		showStream?: boolean;
		showTimestamp?: boolean;
		maxLines?: number;
	}
	let {
		topic,
		seed = [],
		height = '24rem',
		showStream = true,
		showTimestamp = true,
		maxLines = 2000
	}: Props = $props();

	let lines = $state<LineRecord[]>(seed.slice(-maxLines));
	let filter = $state('');
	let timestamps = $state(showTimestamp);
	let autoScroll = $state(true);
	let scroller: HTMLDivElement | null = null;
	let unsub: (() => void) | null = null;

	function append(rec: LineRecord) {
		lines = [...lines, rec].slice(-maxLines);
		queueMicrotask(() => {
			if (autoScroll && scroller) {
				scroller.scrollTop = scroller.scrollHeight;
			}
		});
	}

	function onUserScroll() {
		if (!scroller) return;
		const atBottom = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 8;
		autoScroll = atBottom;
	}

	const visible = $derived(
		filter
			? lines.filter((l) => (l.line ?? l.l ?? '').toLowerCase().includes(filter.toLowerCase()))
			: lines
	);

	function fmtTime(t: string): string {
		try {
			const d = new Date(t);
			return d.toISOString().slice(11, 23); // HH:MM:SS.mmm
		} catch {
			return t;
		}
	}

	onMount(() => {
		unsub = subscribe(topic, (data) => {
			if (data && typeof data === 'object') {
				append(data as LineRecord);
			}
		});
	});

	onDestroy(() => {
		unsub?.();
	});

	$effect(() => {
		// Re-seed when the parent passes a different topic + new seed combo.
		// Take the topic as the trigger; clear the buffer so we don't mix
		// containers' lines.
		topic;
		lines = (seed ?? []).slice(-maxLines);
	});
</script>

<div class="space-y-2">
	<div class="flex items-center gap-2 text-sm">
		<input
			type="text"
			placeholder="Filter…"
			bind:value={filter}
			class="flex-1 rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] px-2.5 py-1 text-xs text-[var(--color-fg)] placeholder-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
		/>
		<label class="flex items-center gap-1 text-xs text-[var(--color-fg-muted)]">
			<input
				type="checkbox"
				bind:checked={timestamps}
				class="h-3.5 w-3.5 rounded border-[var(--color-border-strong)] text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
			/>
			Timestamps
		</label>
		<label class="flex items-center gap-1 text-xs text-[var(--color-fg-muted)]">
			<input
				type="checkbox"
				bind:checked={autoScroll}
				class="h-3.5 w-3.5 rounded border-[var(--color-border-strong)] text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
			/>
			Follow
		</label>
		<span class="text-xs text-[var(--color-fg-subtle)]">
			{visible.length} / {lines.length}
		</span>
	</div>
	<div
		bind:this={scroller}
		onscroll={onUserScroll}
		style:height
		class="overflow-auto rounded-md border border-[var(--color-border)] bg-[#0b0b0e] p-2 font-mono text-xs leading-relaxed text-zinc-100"
	>
		{#each visible as l, i (i)}
			<div class="whitespace-pre-wrap break-all">
				{#if timestamps}
					<span class="mr-2 text-zinc-500">{fmtTime(l.t)}</span>
				{/if}
				{#if showStream && l.s}
					<span class="mr-1 {l.s === 'stderr' ? 'text-red-400' : 'text-teal-300'}">{l.s}</span>
				{/if}
				{#if l.phase}
					<span class="text-amber-300">phase: {l.phase}</span>
				{:else}
					<span>{l.line ?? l.l ?? ''}</span>
				{/if}
			</div>
		{/each}
	</div>
</div>
