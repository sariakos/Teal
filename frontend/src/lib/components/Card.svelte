<script lang="ts">
	import type { Snippet } from 'svelte';
	interface Props {
		title?: string;
		description?: string;
		actions?: Snippet;
		padded?: boolean;
		class?: string;
		children: Snippet;
	}
	let {
		title = '',
		description = '',
		actions,
		padded = true,
		class: extraClass = '',
		children
	}: Props = $props();
</script>

<section
	class="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-card)] {extraClass}"
>
	{#if title || actions}
		<header
			class="flex items-start justify-between gap-3 border-b border-[var(--color-border)] px-5 py-3"
		>
			<div class="min-w-0">
				{#if title}
					<h2 class="text-sm font-semibold text-[var(--color-fg)]">{title}</h2>
				{/if}
				{#if description}
					<p class="mt-0.5 text-xs text-[var(--color-fg-muted)]">{description}</p>
				{/if}
			</div>
			{#if actions}
				<div class="flex shrink-0 items-center gap-2">{@render actions()}</div>
			{/if}
		</header>
	{/if}
	<div class={padded ? 'p-5' : ''}>
		{@render children()}
	</div>
</section>
