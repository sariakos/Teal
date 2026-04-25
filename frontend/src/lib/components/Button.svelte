<!--
  Button primitive.

  Variants: primary (filled accent), secondary (bordered), ghost (no
  background, hover bg only), danger (filled danger). Sizes: sm, md.
  Uses semantic tokens so dark mode works without further changes.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	interface Props {
		type?: 'button' | 'submit';
		variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
		size?: 'sm' | 'md';
		disabled?: boolean;
		title?: string;
		onclick?: (e: MouseEvent) => void;
		class?: string;
		children: Snippet;
	}
	let {
		type = 'button',
		variant = 'primary',
		size = 'md',
		disabled = false,
		title,
		onclick,
		class: extraClass = '',
		children
	}: Props = $props();

	const sizes = {
		sm: 'px-2.5 py-1.5 text-xs gap-1.5',
		md: 'px-3.5 py-2 text-sm gap-2'
	} as const;

	const variants = {
		primary:
			'bg-[var(--color-accent)] text-[var(--color-accent-fg)] hover:bg-[var(--color-accent-hover)] focus-visible:ring-[var(--color-accent)] shadow-[var(--shadow-xs)]',
		secondary:
			'border border-[var(--color-border-strong)] bg-[var(--color-surface)] text-[var(--color-fg)] hover:bg-[var(--color-surface-hover)] focus-visible:ring-[var(--color-accent)]',
		ghost:
			'text-[var(--color-fg-muted)] hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-fg)] focus-visible:ring-[var(--color-accent)]',
		danger:
			'bg-[var(--color-danger)] text-[var(--color-danger-fg)] hover:bg-[var(--color-danger-hover)] focus-visible:ring-[var(--color-danger)] shadow-[var(--shadow-xs)]'
	} as const;

	const base =
		'inline-flex items-center justify-center rounded-md font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--color-bg)] disabled:opacity-50 disabled:pointer-events-none';
</script>

<button
	{type}
	{disabled}
	{title}
	{onclick}
	class="{base} {sizes[size]} {variants[variant]} {extraClass}"
>
	{@render children()}
</button>
