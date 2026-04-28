<script lang="ts">
	/*
	 * Live log viewer. Subscribes to one or more realtime topics and
	 * renders a scrollable buffer of N lines. Used by:
	 *   - Container logs tab on the App detail page (one topic per
	 *     container, multiplexed into a single stream)
	 *   - In-flight + just-finished deploy panel (topic "deploy.<id>",
	 *     seeded from the on-disk deploy.log)
	 *
	 * Two ways to seed history:
	 *   - `seed`: caller pre-fetches and passes records (used by the
	 *     Logs tab where history must be merged across containers).
	 *   - `backfillUrl`: LogStream fetches plain-text history itself,
	 *     subscribing FIRST so live lines that arrive during the fetch
	 *     are buffered and flushed afterwards (used by the deploy panel).
	 */
	import { onDestroy, untrack } from 'svelte';
	import { subscribe } from '$lib/realtime/socket';

	export interface LineRecord {
		t?: string; // ISO-8601, optional (deploy.log lines have no per-line timestamp)
		s?: string; // 'stdout' | 'stderr'
		l?: string; // payload from container logs
		line?: string; // payload from deploy logs
		phase?: string; // phase change events on deploy.<id>
		// Optional source-tag for unified multi-container view. Rendered
		// as a colored prefix when present.
		source?: { label: string; tone?: 'blue' | 'green' | 'neutral' };
	}

	interface TopicSource {
		topic: string;
		// Stamp incoming lines with a source tag so the unified view can
		// render "<service> | <line>". Omit for single-source topics.
		source?: { label: string; tone?: 'blue' | 'green' | 'neutral' };
	}

	interface Props {
		// Either a single topic string or a list of topic sources for
		// the multi-container unified view.
		topic?: string;
		topics?: TopicSource[];
		seed?: LineRecord[];
		// If set, LogStream fetches this URL as plain text on mount and
		// prepends every non-empty line as a `{line}` record. Used to
		// hydrate from the deploy.log file before live lines start flowing.
		backfillUrl?: string;
		height?: string;
		showStream?: boolean;
		showTimestamp?: boolean;
		maxLines?: number;
		// Optional filter over a record's `source.label`. If set, only
		// matching records render. Used by the Logs tab to implement
		// the "show only these containers" multi-select.
		filterSources?: string[] | null;
	}
	let {
		topic,
		topics,
		seed = [],
		backfillUrl,
		height = '24rem',
		showStream = true,
		showTimestamp = true,
		maxLines = 2000,
		filterSources = null
	}: Props = $props();

	// Initial values are read via untrack: the seed/maxLines/showTimestamp
	// props are reactive, but here we want one-shot initialization. The
	// $effect below re-seeds `lines` on every topic change, so this is
	// only used until that runs.
	let lines = $state<LineRecord[]>(untrack(() => seed.slice(-maxLines)));
	let filter = $state('');
	let timestamps = $state(untrack(() => showTimestamp));
	let autoScroll = $state(true);
	let scroller: HTMLDivElement | null = null;
	let unsubs: Array<() => void> = [];

	// Effective topic list. A `topic` prop becomes a single-source list
	// with no source tag.
	const topicList = $derived<TopicSource[]>(
		topics && topics.length > 0
			? topics
			: topic
				? [{ topic }]
				: []
	);
	const topicKey = $derived(topicList.map((t) => t.topic).join('|'));

	function append(rec: LineRecord) {
		lines = [...lines, rec].slice(-maxLines);
		queueMicrotask(() => {
			if (autoScroll && scroller) {
				scroller.scrollTop = scroller.scrollHeight;
			}
		});
	}

	function appendBatch(batch: LineRecord[]) {
		if (batch.length === 0) return;
		lines = [...lines, ...batch].slice(-maxLines);
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
		lines.filter((l) => {
			if (filterSources && l.source && !filterSources.includes(l.source.label)) {
				return false;
			}
			if (!filter) return true;
			const text = l.line ?? l.l ?? '';
			return text.toLowerCase().includes(filter.toLowerCase());
		})
	);

	function fmtTime(t: string | undefined): string {
		if (!t) return '';
		try {
			const d = new Date(t);
			return d.toISOString().slice(11, 23);
		} catch {
			return t;
		}
	}

	function toneClass(tone: string | undefined): string {
		switch (tone) {
			case 'blue':
				return 'text-sky-300';
			case 'green':
				return 'text-emerald-300';
			default:
				return 'text-zinc-400';
		}
	}

	// Subscription + backfill management. Re-runs whenever the set of
	// topics changes. Each pass: subscribe to all topics first, buffer
	// incoming events while the (optional) backfillUrl fetch resolves,
	// then concat history + buffered + dedupe + flush, then enter the
	// steady-state where new events flow straight to the buffer.
	$effect(() => {
		topicKey; // re-trigger when topic set changes
		// Reset buffer + tear down old subs. Read seed/topicList without
		// tracking so a parent re-fetching `seed` doesn't tear down the
		// live subscription.
		const initialSeed = untrack(() => seed);
		const sources = untrack(() => topicList);
		const url = untrack(() => backfillUrl);
		for (const u of unsubs) u();
		unsubs = [];
		lines = (initialSeed ?? []).slice(-maxLines);

		if (sources.length === 0) return;

		let opened = false;
		const buffered: LineRecord[] = [];

		for (const src of sources) {
			const u = subscribe(src.topic, (data) => {
				if (!data || typeof data !== 'object') return;
				const rec: LineRecord = { ...(data as LineRecord) };
				if (src.source) rec.source = src.source;
				if (opened) {
					append(rec);
				} else {
					buffered.push(rec);
				}
			});
			unsubs.push(u);
		}

		const open = (history: LineRecord[]) => {
			if (opened) return;
			opened = true;
			// Dedupe: backfill comes from the on-disk file, but the same
			// lines may have been re-published live during the fetch
			// window. Drop any buffered line whose payload + (rounded)
			// timestamp matches a history line.
			const seen = new Set<string>();
			for (const h of history) {
				seen.add(`${h.t ?? ''}|${h.line ?? h.l ?? ''}`);
			}
			const fresh = buffered.filter((b) => !seen.has(`${b.t ?? ''}|${b.line ?? b.l ?? ''}`));
			appendBatch([...history, ...fresh]);
		};

		if (url) {
			fetch(url, { credentials: 'include' })
				.then(async (res) => {
					if (!res.ok) {
						open([]);
						return;
					}
					const text = await res.text();
					const history: LineRecord[] = text
						.split('\n')
						.filter((l) => l.length > 0)
						.map((l) => ({ line: l }));
					open(history);
				})
				.catch(() => {
					open([]);
				});
		} else {
			open([]);
		}

		return () => {
			for (const u of unsubs) u();
			unsubs = [];
		};
	});

	onDestroy(() => {
		for (const u of unsubs) u();
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
				{#if timestamps && l.t}
					<span class="mr-2 text-zinc-500">{fmtTime(l.t)}</span>
				{/if}
				{#if l.source}
					<span class="mr-2 {toneClass(l.source.tone)}">{l.source.label}</span>
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
