<!--
  Horizontal tabs. Caller passes a `tabs` array and binds `value`. The
  active style sits at the bottom border so it works with both the
  default light surface and dark mode.
-->
<script lang="ts" generics="T extends string">
	interface TabDef {
		value: T;
		label: string;
		count?: number;
	}
	interface Props {
		tabs: TabDef[];
		value: T;
		class?: string;
	}
	let { tabs, value = $bindable(), class: extraClass = '' }: Props = $props();
</script>

<div class="border-b border-[var(--color-border)] {extraClass}">
	<nav class="-mb-px flex gap-6 overflow-x-auto" aria-label="Tabs">
		{#each tabs as t}
			<button
				type="button"
				onclick={() => (value = t.value)}
				class="-mb-px flex items-center gap-1.5 border-b-2 pb-2.5 pt-1 text-sm font-medium transition-colors {value ===
				t.value
					? 'border-[var(--color-accent)] text-[var(--color-fg)]'
					: 'border-transparent text-[var(--color-fg-muted)] hover:border-[var(--color-border-strong)] hover:text-[var(--color-fg)]'}"
			>
				{t.label}
				{#if t.count !== undefined && t.count > 0}
					<span
						class="inline-flex items-center rounded-full bg-[var(--color-surface-muted)] px-1.5 py-0.5 text-[10px] text-[var(--color-fg-muted)]"
					>
						{t.count}
					</span>
				{/if}
			</button>
		{/each}
	</nav>
</div>
