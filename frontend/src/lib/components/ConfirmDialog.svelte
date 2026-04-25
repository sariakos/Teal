<!--
  Global confirm/prompt modal. Mounted once in +layout.svelte; reads from
  the dialog store. Only renders when a request is queued. Esc cancels;
  Enter confirms (when the input passes any `expect` guard for prompts).
-->
<script lang="ts">
	import { dialog } from '$lib/stores/dialog.svelte';
	import { AlertTriangle, AlertCircle, HelpCircle } from '@lucide/svelte';

	let inputEl = $state<HTMLInputElement | null>(null);
	let inputValue = $state('');

	const current = $derived(dialog.current);
	const isPrompt = $derived(current?.request.kind === 'prompt');
	const tone = $derived(current?.request.tone ?? 'default');

	const expectMatches = $derived.by(() => {
		if (!current || current.request.kind !== 'prompt') return true;
		const expect = current.request.expect;
		if (!expect) return inputValue.length > 0;
		return inputValue === expect;
	});

	const toneIcon = $derived(
		tone === 'danger' ? AlertCircle : tone === 'warning' ? AlertTriangle : HelpCircle
	);
	const toneIconClass = $derived(
		tone === 'danger'
			? 'text-[var(--color-danger)]'
			: tone === 'warning'
				? 'text-[var(--color-warning)]'
				: 'text-[var(--color-fg-muted)]'
	);
	const confirmBtnClass = $derived(
		tone === 'danger'
			? 'bg-[var(--color-danger)] hover:bg-[var(--color-danger-hover)] text-[var(--color-danger-fg)] focus-visible:ring-[var(--color-danger)]'
			: 'bg-[var(--color-accent)] hover:bg-[var(--color-accent-hover)] text-[var(--color-accent-fg)] focus-visible:ring-[var(--color-accent)]'
	);

	$effect(() => {
		if (!current) return;
		// Seed prompt state from the new request.
		inputValue = current.request.kind === 'prompt' ? (current.request.initial ?? '') : '';
		// Focus on next tick so the bound element exists.
		queueMicrotask(() => {
			inputEl?.focus();
			inputEl?.select();
		});
	});

	function cancel() {
		if (!current) return;
		dialog.resolve(current.id, isPrompt ? null : false);
	}

	function confirm() {
		if (!current) return;
		if (isPrompt) {
			if (!expectMatches) return;
			dialog.resolve(current.id, inputValue);
		} else {
			dialog.resolve(current.id, true);
		}
	}

	function onKeydown(e: KeyboardEvent) {
		if (!current) return;
		if (e.key === 'Escape') {
			e.preventDefault();
			cancel();
		} else if (e.key === 'Enter' && (!isPrompt || expectMatches)) {
			// Prompt enter is handled by the input itself; confirm dialogs
			// use this global handler.
			if (!isPrompt) {
				e.preventDefault();
				confirm();
			}
		}
	}
</script>

<svelte:window onkeydown={onKeydown} />

{#if current}
	{@const Icon = toneIcon}
	<div
		class="fixed inset-0 z-[1100] flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
		style="animation: overlay-in 120ms ease-out both;"
		onclick={(e) => {
			if (e.target === e.currentTarget) cancel();
		}}
		role="presentation"
	>
		<div
			class="w-full max-w-md rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-6 shadow-[var(--shadow-modal)]"
			style="animation: dialog-in 160ms cubic-bezier(0.2, 0.9, 0.3, 1.1) both;"
			role="dialog"
			aria-modal="true"
			aria-labelledby="confirm-dialog-title"
		>
			<div class="flex items-start gap-4">
				<div
					class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-[var(--color-surface-muted)]"
				>
					<Icon class="h-5 w-5 {toneIconClass}" />
				</div>
				<div class="min-w-0 flex-1">
					<h2
						id="confirm-dialog-title"
						class="text-base font-semibold text-[var(--color-fg)]"
					>
						{current.request.title}
					</h2>
					{#if current.request.body}
						<p class="mt-1 text-sm text-[var(--color-fg-muted)]">
							{current.request.body}
						</p>
					{/if}
					{#if isPrompt && current.request.kind === 'prompt'}
						<input
							bind:this={inputEl}
							bind:value={inputValue}
							placeholder={current.request.placeholder ?? ''}
							onkeydown={(e) => {
								if (e.key === 'Enter' && expectMatches) {
									e.preventDefault();
									confirm();
								}
							}}
							class="mt-3 block w-full rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-fg)] placeholder-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
						/>
						{#if current.request.help}
							<p class="mt-2 text-xs text-[var(--color-fg-subtle)]">
								{current.request.help}
							</p>
						{/if}
						{#if current.request.expect}
							<p class="mt-2 font-mono text-xs text-[var(--color-fg-subtle)]">
								Type <span class="text-[var(--color-fg-muted)]">{current.request.expect}</span> to confirm
							</p>
						{/if}
					{/if}
				</div>
			</div>
			<div class="mt-6 flex justify-end gap-2">
				<button
					type="button"
					onclick={cancel}
					class="rounded-md border border-[var(--color-border-strong)] bg-[var(--color-surface)] px-4 py-2 text-sm font-medium text-[var(--color-fg)] transition-colors hover:bg-[var(--color-surface-hover)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
				>
					{current.request.cancelLabel ?? 'Cancel'}
				</button>
				<button
					type="button"
					onclick={confirm}
					disabled={isPrompt && !expectMatches}
					class="rounded-md px-4 py-2 text-sm font-medium transition-colors focus:outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50 {confirmBtnClass}"
				>
					{current.request.confirmLabel ?? (tone === 'danger' ? 'Delete' : 'Confirm')}
				</button>
			</div>
		</div>
	</div>
{/if}
