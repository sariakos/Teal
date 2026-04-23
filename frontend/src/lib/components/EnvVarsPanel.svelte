<script lang="ts">
	/*
	 * Per-app env vars + shared-env allow-list, with masked-by-default values
	 * and a single "Reveal all" toggle that triggers an audited backend
	 * reveal. Used inside the App detail page.
	 */
	import { onMount } from 'svelte';
	import { ApiError } from '$lib/api/client';
	import { appEnvVarsApi, appSharedEnvVarsApi } from '$lib/api/envvars';
	import type { AppSharedListing, EnvVarRow } from '$lib/api/types';
	import Button from './Button.svelte';
	import Card from './Card.svelte';
	import Input from './Input.svelte';

	let { slug }: { slug: string } = $props();

	let rows = $state<EnvVarRow[]>([]);
	let loading = $state(true);
	let revealed = $state(false);
	let listError = $state<string | null>(null);

	let newKey = $state('');
	let newValue = $state('');
	let saving = $state(false);
	let formError = $state<string | null>(null);

	let sharedListing = $state<AppSharedListing>({ included: [], available: [] });
	let savingShared = $state(false);
	let sharedError = $state<string | null>(null);

	async function reload(reveal = false) {
		loading = true;
		listError = null;
		try {
			rows = await appEnvVarsApi.list(slug, reveal);
			revealed = reveal;
		} catch (err) {
			listError = err instanceof Error ? err.message : 'Failed to load env vars';
		} finally {
			loading = false;
		}
	}

	async function reloadShared() {
		try {
			sharedListing = await appSharedEnvVarsApi.list(slug);
		} catch {
			// Non-fatal — leave the existing listing.
		}
	}

	async function addEnvVar() {
		if (!newKey) return;
		saving = true;
		formError = null;
		try {
			await appEnvVarsApi.upsert(slug, newKey, newValue);
			newKey = '';
			newValue = '';
			await reload(revealed);
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function deleteEnvVar(key: string) {
		if (!confirm(`Delete env var "${key}"?`)) return;
		try {
			await appEnvVarsApi.remove(slug, key);
			await reload(revealed);
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Delete failed');
		}
	}

	function toggleSharedKey(key: string, include: boolean) {
		const set = new Set(sharedListing.included);
		if (include) set.add(key);
		else set.delete(key);
		void persistShared([...set].sort());
	}

	async function persistShared(keys: string[]) {
		savingShared = true;
		sharedError = null;
		try {
			await appSharedEnvVarsApi.set(slug, keys);
			await reloadShared();
		} catch (err) {
			sharedError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			savingShared = false;
		}
	}

	onMount(async () => {
		await Promise.all([reload(false), reloadShared()]);
	});
</script>

<div class="space-y-6">
	<Card title="Per-app environment variables">
		{#if listError}
			<div class="text-sm text-red-600">{listError}</div>
		{:else if loading}
			<div class="text-sm text-zinc-500">Loading…</div>
		{:else}
			<div class="mb-3 flex items-center justify-between">
				<p class="text-sm text-zinc-500">
					Injected into the container as KEY=VALUE on every deploy.
				</p>
				<Button
					variant="secondary"
					onclick={() => reload(!revealed)}
				>
					{revealed ? 'Hide values' : 'Reveal values (audited)'}
				</Button>
			</div>

			{#if rows.length === 0}
				<p class="text-sm text-zinc-500">No env vars configured.</p>
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
										onclick={() => deleteEnvVar(r.key)}
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
					void addEnvVar();
				}}
			>
				<div>
					<label for="newkey" class="mb-1 block text-xs text-zinc-500">Key</label>
					<Input id="newkey" bind:value={newKey} placeholder="DATABASE_URL" />
				</div>
				<div>
					<label for="newvalue" class="mb-1 block text-xs text-zinc-500">Value</label>
					<Input id="newvalue" bind:value={newValue} placeholder="postgres://…" />
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

	<Card title="Shared env vars">
		<p class="mb-3 text-sm text-zinc-500">
			Pick which platform-wide shared keys this app should receive. Per-app keys above shadow
			shared keys with the same name.
		</p>
		{#if sharedListing.available.length === 0}
			<p class="text-sm text-zinc-500">No shared env vars are defined yet (admin sets them).</p>
		{:else}
			<ul class="space-y-1 text-sm">
				{#each sharedListing.available as key}
					<li class="flex items-center gap-2">
						<input
							type="checkbox"
							id={`shared-${key}`}
							checked={sharedListing.included.includes(key)}
							onchange={(e) =>
								toggleSharedKey(key, (e.currentTarget as HTMLInputElement).checked)}
							disabled={savingShared}
						/>
						<label class="font-mono" for={`shared-${key}`}>{key}</label>
					</li>
				{/each}
			</ul>
			{#if sharedListing.included.some((k) => !sharedListing.available.includes(k))}
				<p class="mt-3 text-sm text-amber-700">
					{sharedListing.included.filter((k) => !sharedListing.available.includes(k)).length} opted-in
					key(s) no longer exist as shared vars; they'll be skipped on deploy.
				</p>
			{/if}
			{#if sharedError}
				<div class="mt-2 text-sm text-red-600">{sharedError}</div>
			{/if}
		{/if}
	</Card>
</div>
