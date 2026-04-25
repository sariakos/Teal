<!--
  Reusable empty state. Pass an icon component (lucide), a title, an
  optional description, and an optional `action` snippet for the CTA.
-->
<script lang="ts">
	import type { Snippet, Component } from 'svelte';

	interface Props {
		icon?: Component;
		title: string;
		description?: string;
		action?: Snippet;
		compact?: boolean;
	}
	let { icon: IconComp, title, description = '', action, compact = false }: Props = $props();
</script>

<div
	class="flex flex-col items-center justify-center rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-bg-subtle)] text-center {compact
		? 'px-4 py-8'
		: 'px-6 py-12'}"
>
	{#if IconComp}
		<div
			class="mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[var(--color-surface)] text-[var(--color-fg-muted)] shadow-[var(--shadow-xs)]"
		>
			<IconComp class="h-5 w-5" />
		</div>
	{/if}
	<h3 class="text-sm font-semibold text-[var(--color-fg)]">{title}</h3>
	{#if description}
		<p class="mt-1 max-w-md text-sm text-[var(--color-fg-muted)]">{description}</p>
	{/if}
	{#if action}
		<div class="mt-4">{@render action()}</div>
	{/if}
</div>
