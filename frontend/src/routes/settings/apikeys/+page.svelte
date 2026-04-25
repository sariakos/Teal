<script lang="ts">
	import { onMount } from 'svelte';
	import { apiKeysApi } from '$lib/api/apikeys';
	import { ApiError } from '$lib/api/client';
	import type { ApiKey } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';

	let keys = $state<ApiKey[]>([]);
	let loading = $state(true);
	let listError = $state<string | null>(null);

	let newName = $state('');
	let creating = $state(false);
	let createError = $state<string | null>(null);

	// Newly issued raw key, surfaced once and then cleared. Stored in
	// component state (not the global store) so it disappears on navigation.
	let revealedKey = $state<string | null>(null);

	async function reload() {
		try {
			keys = await apiKeysApi.list();
			listError = null;
		} catch (err) {
			listError = err instanceof Error ? err.message : 'Failed to load keys';
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	async function handleCreate(e: SubmitEvent) {
		e.preventDefault();
		createError = null;
		creating = true;
		try {
			const resp = await apiKeysApi.create(newName);
			revealedKey = resp.key;
			newName = '';
			await reload();
		} catch (err) {
			createError = err instanceof ApiError ? err.message : 'Create failed';
		} finally {
			creating = false;
		}
	}

	async function handleRevoke(key: ApiKey) {
		if (
			!(await dialog.confirm({
				title: `Revoke "${key.name}"?`,
				body: 'Any script or pipeline using this key will start receiving 401s on the next request.',
				tone: 'danger',
				confirmLabel: 'Revoke'
			}))
		)
			return;
		try {
			await apiKeysApi.revoke(key.id);
			await reload();
			toast.success(`Revoked "${key.name}"`);
		} catch (err) {
			toast.error('Revoke failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	async function copyToClipboard(value: string) {
		try {
			await navigator.clipboard.writeText(value);
		} catch {
			// Best-effort; the visible value is still selectable.
		}
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">API Keys</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Use these for CI/CD or scripting. Keys act with your own permissions.
		</p>
	</div>

	{#if revealedKey}
		<Card title="Copy this key now — it will not be shown again">
			<div class="flex items-center gap-2">
				<code class="flex-1 break-all rounded bg-zinc-900 px-3 py-2 text-sm text-teal-300">
					{revealedKey}
				</code>
				<Button variant="secondary" onclick={() => copyToClipboard(revealedKey!)}>Copy</Button>
				<Button variant="secondary" onclick={() => (revealedKey = null)}>Dismiss</Button>
			</div>
		</Card>
	{/if}

	<Card title="Issue new key">
		<form onsubmit={handleCreate} class="grid gap-3 sm:grid-cols-[3fr_auto]">
			<Input placeholder="Name (e.g. ci-deploy)" required bind:value={newName} />
			<Button type="submit" disabled={creating}>{creating ? 'Issuing…' : 'Issue key'}</Button>
		</form>
		{#if createError}
			<div class="mt-3 text-sm text-red-600">{createError}</div>
		{/if}
	</Card>

	<Card title="Existing keys">
		{#if loading}
			<div class="text-sm text-zinc-500">Loading…</div>
		{:else if listError}
			<div class="text-sm text-red-600">{listError}</div>
		{:else if keys.length === 0}
			<div class="text-sm text-zinc-500">No keys yet.</div>
		{:else}
			<table class="w-full text-sm">
				<thead class="text-left text-xs uppercase text-zinc-500">
					<tr>
						<th class="pb-2">Name</th>
						<th class="pb-2">Last used</th>
						<th class="pb-2">Created</th>
						<th class="pb-2">Status</th>
						<th class="pb-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each keys as key}
						<tr class="border-t border-zinc-100">
							<td class="py-2 font-medium text-zinc-800">{key.name}</td>
							<td class="py-2 text-zinc-500">
								{key.lastUsedAt ? new Date(key.lastUsedAt).toLocaleString() : 'Never'}
							</td>
							<td class="py-2 text-zinc-500">{new Date(key.createdAt).toLocaleDateString()}</td>
							<td class="py-2">
								{#if key.revokedAt}
									<span class="text-xs text-zinc-400">revoked</span>
								{:else}
									<span class="rounded-full bg-teal-50 px-2 py-0.5 text-xs text-teal-700">
										active
									</span>
								{/if}
							</td>
							<td class="py-2 text-right">
								{#if !key.revokedAt}
									<button
										class="text-sm text-red-600 hover:underline"
										onclick={() => handleRevoke(key)}
									>
										Revoke
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</Card>
</div>
