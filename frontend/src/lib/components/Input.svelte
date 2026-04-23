<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props {
		type?: 'text' | 'email' | 'password';
		value: string;
		placeholder?: string;
		required?: boolean;
		autocomplete?: HTMLInputAttributes['autocomplete'];
		id?: string;
		class?: string;
	}
	let {
		type = 'text',
		value = $bindable(),
		placeholder = '',
		required = false,
		autocomplete,
		id = '',
		class: extraClass = ''
	}: Props = $props();
</script>

<input
	{type}
	{id}
	{placeholder}
	{required}
	{autocomplete}
	bind:value
	class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm placeholder-zinc-400 focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500 {extraClass}"
/>
