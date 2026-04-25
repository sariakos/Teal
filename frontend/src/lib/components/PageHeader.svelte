<script lang="ts">
	import type { Snippet } from 'svelte';
	interface Props {
		title: string;
		description?: string;
		eyebrow?: string;
		actions?: Snippet;
		extra?: Snippet;
	}
	let { title, description = '', eyebrow = '', actions, extra }: Props = $props();
</script>

<div class="flex items-start justify-between gap-4">
	<div class="min-w-0">
		{#if eyebrow}
			<div class="text-xs font-medium uppercase tracking-wide text-[var(--color-fg-subtle)]">
				{eyebrow}
			</div>
		{/if}
		<div class="flex items-center gap-2">
			<h1 class="text-2xl font-semibold tracking-tight text-[var(--color-fg)]">{title}</h1>
			{#if extra}{@render extra()}{/if}
		</div>
		{#if description}
			<p class="mt-1 text-sm text-[var(--color-fg-muted)]">{description}</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex shrink-0 items-center gap-2">{@render actions()}</div>
	{/if}
</div>
