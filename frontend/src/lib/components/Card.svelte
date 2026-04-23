<script lang="ts">
	import type { Snippet } from 'svelte';
	interface Props {
		title?: string;
		children: Snippet;
	}
	let { title = '', children }: Props = $props();
</script>

<div class="rounded-lg border border-zinc-200 bg-white shadow-sm">
	{#if title}
		<div class="border-b border-zinc-200 px-5 py-3">
			<h2 class="text-sm font-medium text-zinc-700">{title}</h2>
		</div>
	{/if}
	<div class="p-5">
		{@render children()}
	</div>
</div>
