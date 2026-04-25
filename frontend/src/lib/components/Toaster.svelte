<!--
  Mounted once in +layout.svelte. Renders the global toast queue in a
  fixed bottom-right stack. Each toast animates in from below and fades
  out on dismiss; auto-dismiss is handled by the store, this component
  only renders.
-->
<script lang="ts">
	import { toast } from '$lib/stores/toast.svelte';
	import { CheckCircle2, AlertCircle, AlertTriangle, Info, X } from '@lucide/svelte';

	const iconFor = {
		success: CheckCircle2,
		error: AlertCircle,
		warning: AlertTriangle,
		info: Info
	} as const;

	const accentFor = {
		success: 'text-[var(--color-success)]',
		error: 'text-[var(--color-danger)]',
		warning: 'text-[var(--color-warning)]',
		info: 'text-[var(--color-info)]'
	} as const;
</script>

<div
	class="pointer-events-none fixed inset-0 z-[1000] flex flex-col items-end justify-end gap-2 p-4 sm:p-6"
	aria-live="polite"
	aria-atomic="false"
>
	{#each toast.items as item (item.id)}
		{@const Icon = iconFor[item.level]}
		<div
			class="pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] p-3 shadow-[var(--shadow-popover)]"
			style="animation: toast-in 180ms cubic-bezier(0.2, 0.9, 0.3, 1.2) both;"
			role={item.level === 'error' ? 'alert' : 'status'}
		>
			<Icon class="mt-0.5 h-5 w-5 shrink-0 {accentFor[item.level]}" />
			<div class="min-w-0 flex-1">
				<div class="text-sm font-medium text-[var(--color-fg)]">{item.title}</div>
				{#if item.description}
					<div class="mt-0.5 text-xs text-[var(--color-fg-muted)] break-words">
						{item.description}
					</div>
				{/if}
			</div>
			<button
				class="-m-1 rounded p-1 text-[var(--color-fg-subtle)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-fg)]"
				onclick={() => toast.dismiss(item.id)}
				aria-label="Dismiss"
			>
				<X class="h-4 w-4" />
			</button>
		</div>
	{/each}
</div>
