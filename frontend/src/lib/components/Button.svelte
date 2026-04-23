<!--
  Minimal button primitive. Variants: primary (teal), secondary (outline),
  danger. Size is implicit; tweak via class prop for one-offs.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	interface Props {
		type?: 'button' | 'submit';
		variant?: 'primary' | 'secondary' | 'danger';
		disabled?: boolean;
		onclick?: (e: MouseEvent) => void;
		class?: string;
		children: Snippet;
	}
	let {
		type = 'button',
		variant = 'primary',
		disabled = false,
		onclick,
		class: extraClass = '',
		children
	}: Props = $props();

	const base =
		'inline-flex items-center justify-center rounded-md px-4 py-2 text-sm font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-teal-500 disabled:opacity-50 disabled:pointer-events-none';

	const variants = {
		primary: 'bg-teal-600 text-white hover:bg-teal-700',
		secondary: 'border border-zinc-300 bg-white hover:bg-zinc-50 text-zinc-900',
		danger: 'bg-red-600 text-white hover:bg-red-700'
	} as const;
</script>

<button
	{type}
	{disabled}
	{onclick}
	class="{base} {variants[variant]} {extraClass}"
>
	{@render children()}
</button>
