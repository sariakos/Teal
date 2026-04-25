<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props {
		type?: 'text' | 'email' | 'password' | 'number' | 'url' | 'search';
		value: string;
		placeholder?: string;
		required?: boolean;
		disabled?: boolean;
		autocomplete?: HTMLInputAttributes['autocomplete'];
		id?: string;
		mono?: boolean;
		size?: 'sm' | 'md';
		class?: string;
	}
	let {
		type = 'text',
		value = $bindable(),
		placeholder = '',
		required = false,
		disabled = false,
		autocomplete,
		id = '',
		mono = false,
		size = 'md',
		class: extraClass = ''
	}: Props = $props();

	const sizes = {
		sm: 'px-2.5 py-1.5 text-xs',
		md: 'px-3 py-2 text-sm'
	} as const;
</script>

<input
	{type}
	{id}
	{placeholder}
	{required}
	{disabled}
	{autocomplete}
	bind:value
	class="block w-full rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] text-[var(--color-fg)] placeholder-[var(--color-fg-subtle)] transition-colors hover:border-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] disabled:cursor-not-allowed disabled:opacity-50 {mono
		? 'font-mono'
		: ''} {sizes[size]} {extraClass}"
/>
