<!--
  Admin CRUD for platform-wide shared env vars. Apps opt in to specific
  keys via their own Env tab; nothing is auto-injected.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { ApiError } from '$lib/api/client';
	import { sharedEnvVarsApi } from '$lib/api/envvars';
	import type { EnvVarRow } from '$lib/api/types';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Input from '$lib/components/Input.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { Eye, EyeOff, Globe, Trash2 } from '@lucide/svelte';

	let rows = $state<EnvVarRow[]>([]);
	let loading = $state(true);
	let revealed = $state(false);
	let error = $state<string | null>(null);

	let newKey = $state('');
	let newValue = $state('');
	let saving = $state(false);
	let formError = $state<string | null>(null);

	async function reload(reveal = false) {
		loading = true;
		error = null;
		try {
			rows = await sharedEnvVarsApi.list(reveal);
			revealed = reveal;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load shared env vars';
		} finally {
			loading = false;
		}
	}

	async function add() {
		if (!newKey) return;
		saving = true;
		formError = null;
		try {
			await sharedEnvVarsApi.upsert(newKey, newValue);
			const k = newKey;
			newKey = '';
			newValue = '';
			await reload(revealed);
			toast.success(`${k} saved`);
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function remove(key: string) {
		if (
			!(await dialog.confirm({
				title: `Delete shared env "${key}"?`,
				body: 'Apps that opted in will stop receiving this variable on the next deploy.',
				tone: 'danger'
			}))
		)
			return;
		try {
			await sharedEnvVarsApi.remove(key);
			await reload(revealed);
			toast.success(`Deleted ${key}`);
		} catch (err) {
			toast.error('Delete failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	onMount(() => reload(false));
</script>

<div class="space-y-6">
	<PageHeader
		title="Shared env vars"
		description="Available to every app, but each app must explicitly opt in from its own Env tab. Admin only."
	/>

	{#if error}
		<div class="text-sm text-[var(--color-danger)]">{error}</div>
	{:else if loading}
		<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
	{:else}
		{#if rows.length === 0}
			<EmptyState
				icon={Globe}
				title="No shared env vars yet"
				description="Add one below — apps will need to opt in from their own Env tab."
			/>
		{:else}
			<Card padded={false}>
				{#snippet actions()}
					<Button variant="ghost" size="sm" onclick={() => reload(!revealed)}>
						{#if revealed}
							<EyeOff class="h-3.5 w-3.5" />
							Hide values
						{:else}
							<Eye class="h-3.5 w-3.5" />
							Reveal (audited)
						{/if}
					</Button>
				{/snippet}
				<table class="w-full text-sm">
					<thead
						class="text-left text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
					>
						<tr class="border-b border-[var(--color-border)]">
							<th class="px-5 py-2.5">Key</th>
							<th class="px-5 py-2.5">Value</th>
							<th class="px-5 py-2.5">Updated</th>
							<th class="px-5 py-2.5"></th>
						</tr>
					</thead>
					<tbody>
						{#each rows as r}
							<tr
								class="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-bg-subtle)]"
							>
								<td class="px-5 py-2.5 font-mono text-[var(--color-fg)]">{r.key}</td>
								<td class="px-5 py-2.5 font-mono text-xs">
									{#if revealed && r.value !== undefined}
										<code
											class="rounded bg-[var(--color-bg-subtle)] px-2 py-0.5 text-[var(--color-fg)]"
										>
											{r.value}
										</code>
									{:else if r.hasValue}
										<span class="text-[var(--color-fg-subtle)]">••••••</span>
									{:else}
										<span class="text-[var(--color-fg-subtle)]">(empty)</span>
									{/if}
								</td>
								<td class="px-5 py-2.5 text-xs text-[var(--color-fg-muted)]">
									{r.updatedAt ? new Date(r.updatedAt).toLocaleString() : '—'}
								</td>
								<td class="px-5 py-2.5 text-right">
									<button
										class="inline-flex items-center gap-1 text-xs font-medium text-[var(--color-danger)] hover:underline"
										onclick={() => remove(r.key)}
									>
										<Trash2 class="h-3.5 w-3.5" />
										Delete
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</Card>
		{/if}

		<Card title="Add / update">
			<form
				class="grid grid-cols-1 items-end gap-3 sm:grid-cols-[1fr_2fr_auto]"
				onsubmit={(e) => {
					e.preventDefault();
					void add();
				}}
			>
				<div>
					<label for="newkey" class="mb-1 block text-xs font-medium text-[var(--color-fg-muted)]">
						Key
					</label>
					<Input id="newkey" bind:value={newKey} placeholder="SENTRY_DSN" mono />
				</div>
				<div>
					<label
						for="newvalue"
						class="mb-1 block text-xs font-medium text-[var(--color-fg-muted)]"
					>
						Value
					</label>
					<Input id="newvalue" bind:value={newValue} placeholder="https://…" />
				</div>
				<Button type="submit" disabled={saving || !newKey}>
					{saving ? 'Saving…' : 'Save'}
				</Button>
			</form>
			{#if formError}
				<div class="mt-2 text-sm text-[var(--color-danger)]">{formError}</div>
			{/if}
		</Card>
	{/if}
</div>
