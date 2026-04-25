<!--
  Compact label. Tones map to the *-soft semantic tokens so they read
  well in both light and dark modes without per-mode classes.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info';

	interface Props {
		tone?: Tone;
		size?: 'sm' | 'md';
		class?: string;
		children: Snippet;
	}
	let { tone = 'neutral', size = 'md', class: extraClass = '', children }: Props = $props();

	const tones: Record<Tone, string> = {
		neutral:
			'bg-[var(--color-surface-muted)] text-[var(--color-fg-muted)] border-[var(--color-border)]',
		accent:
			'bg-[var(--color-accent-soft)] text-[var(--color-accent-soft-fg)] border-[var(--color-accent-soft)]',
		success:
			'bg-[var(--color-success-soft)] text-[var(--color-success-soft-fg)] border-[var(--color-success-soft)]',
		warning:
			'bg-[var(--color-warning-soft)] text-[var(--color-warning-soft-fg)] border-[var(--color-warning-soft)]',
		danger:
			'bg-[var(--color-danger-soft)] text-[var(--color-danger-soft-fg)] border-[var(--color-danger-soft)]',
		info: 'bg-[var(--color-info-soft)] text-[var(--color-info-soft-fg)] border-[var(--color-info-soft)]'
	};

	const sizes = {
		sm: 'px-1.5 py-0.5 text-[10px]',
		md: 'px-2 py-0.5 text-xs'
	} as const;
</script>

<span
	class="inline-flex items-center gap-1 rounded-full border font-medium {sizes[size]} {tones[
		tone
	]} {extraClass}"
>
	{@render children()}
</span>
