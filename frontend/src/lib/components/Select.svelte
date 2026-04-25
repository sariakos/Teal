<!--
  Styled native <select>. Same visual treatment as Input. Children are
  the <option> tags rendered by the caller — that keeps it simple and
  avoids reinventing accessibility-correct combobox semantics.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	interface Props {
		value: string;
		id?: string;
		disabled?: boolean;
		size?: 'sm' | 'md';
		class?: string;
		onchange?: (e: Event) => void;
		children: Snippet;
	}
	let {
		value = $bindable(),
		id = '',
		disabled = false,
		size = 'md',
		class: extraClass = '',
		onchange,
		children
	}: Props = $props();

	const sizes = {
		sm: 'px-2 py-1 text-xs pr-7',
		md: 'px-3 py-2 text-sm pr-9'
	} as const;
</script>

<select
	{id}
	{disabled}
	bind:value
	{onchange}
	class="block w-full appearance-none rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] bg-no-repeat text-[var(--color-fg)] transition-colors hover:border-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] disabled:cursor-not-allowed disabled:opacity-50 {sizes[
		size
	]} {extraClass}"
	style="background-image: url(&quot;data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 20 20' fill='%23a1a1aa'%3E%3Cpath fill-rule='evenodd' d='M5.23 7.21a.75.75 0 011.06.02L10 11.085l3.71-3.855a.75.75 0 111.08 1.04l-4.25 4.41a.75.75 0 01-1.08 0l-4.25-4.41a.75.75 0 01.02-1.06z' clip-rule='evenodd'/%3E%3C/svg%3E&quot;); background-position: right 0.5rem center; background-size: 1.25em;"
>
	{@render children()}
</select>
