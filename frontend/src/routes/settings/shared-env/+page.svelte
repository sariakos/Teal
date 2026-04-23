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
			newKey = '';
			newValue = '';
			await reload(revealed);
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function remove(key: string) {
		if (!confirm(`Delete shared env "${key}"? Apps that opted in will no longer receive it.`)) return;
		try {
			await sharedEnvVarsApi.remove(key);
			await reload(revealed);
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Delete failed');
		}
	}

	onMount(() => reload(false));
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">Shared env vars</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Available to every app, but each app must explicitly opt in from its own Env tab. Admin only.
		</p>
	</div>

	<Card>
		{#if error}
			<div class="text-sm text-red-600">{error}</div>
		{:else if loading}
			<div class="text-sm text-zinc-500">Loading…</div>
		{:else}
			<div class="mb-3 flex items-center justify-between">
				<p class="text-sm text-zinc-500">
					Values are masked by default. Reveal triggers an audited backend call.
				</p>
				<Button variant="secondary" onclick={() => reload(!revealed)}>
					{revealed ? 'Hide values' : 'Reveal values (audited)'}
				</Button>
			</div>

			{#if rows.length === 0}
				<p class="text-sm text-zinc-500">No shared env vars yet.</p>
			{:else}
				<table class="w-full text-sm">
					<thead class="text-left text-xs uppercase text-zinc-500">
						<tr>
							<th class="pb-2">Key</th>
							<th class="pb-2">Value</th>
							<th class="pb-2">Updated</th>
							<th class="pb-2"></th>
						</tr>
					</thead>
					<tbody>
						{#each rows as r}
							<tr class="border-t border-zinc-100">
								<td class="py-2 font-mono">{r.key}</td>
								<td class="py-2 font-mono text-xs">
									{#if revealed && r.value !== undefined}
										<code class="rounded bg-zinc-50 px-2 py-0.5 text-zinc-800">{r.value}</code>
									{:else if r.hasValue}
										<span class="text-zinc-400">••••••</span>
									{:else}
										<span class="text-zinc-400">(empty)</span>
									{/if}
								</td>
								<td class="py-2 text-zinc-500">
									{r.updatedAt ? new Date(r.updatedAt).toLocaleString() : '—'}
								</td>
								<td class="py-2 text-right">
									<button
										class="text-sm text-red-600 hover:underline"
										onclick={() => remove(r.key)}
									>
										Delete
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}

			<form
				class="mt-4 grid grid-cols-[1fr_2fr_auto] items-end gap-2"
				onsubmit={(e) => {
					e.preventDefault();
					void add();
				}}
			>
				<div>
					<label for="newkey" class="mb-1 block text-xs text-zinc-500">Key</label>
					<Input id="newkey" bind:value={newKey} placeholder="SENTRY_DSN" />
				</div>
				<div>
					<label for="newvalue" class="mb-1 block text-xs text-zinc-500">Value</label>
					<Input id="newvalue" bind:value={newValue} placeholder="https://…" />
				</div>
				<Button type="submit" disabled={saving || !newKey}>
					{saving ? 'Saving…' : 'Add / Update'}
				</Button>
			</form>
			{#if formError}
				<div class="mt-2 text-sm text-red-600">{formError}</div>
			{/if}
		{/if}
	</Card>
</div>
