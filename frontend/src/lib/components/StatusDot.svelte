<script lang="ts">
	type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';

	interface Props {
		tone?: Tone;
		pulse?: boolean;
		class?: string;
	}
	let { tone = 'neutral', pulse = false, class: extraClass = '' }: Props = $props();

	const tones: Record<Tone, string> = {
		neutral: 'bg-[var(--color-fg-subtle)]',
		success: 'bg-[var(--color-success)]',
		warning: 'bg-[var(--color-warning)]',
		danger: 'bg-[var(--color-danger)]',
		info: 'bg-[var(--color-info)]',
		accent: 'bg-[var(--color-accent)]'
	};
</script>

<span class="relative inline-flex h-2 w-2 {extraClass}">
	{#if pulse}
		<span class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60 {tones[tone]}"></span>
	{/if}
	<span class="relative inline-flex h-2 w-2 rounded-full {tones[tone]}"></span>
</span>
