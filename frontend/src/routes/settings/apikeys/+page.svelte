<script lang="ts">
	import { onMount } from 'svelte';
	import { apiKeysApi } from '$lib/api/apikeys';
	import { ApiError } from '$lib/api/client';
	import type { ApiKey } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { KeyRound, Copy, Plus } from '@lucide/svelte';

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
	<PageHeader
		title="API keys"
		description="Use these for CI/CD or scripting. Keys act with your own permissions."
	/>

	{#if revealedKey}
		<Card title="Copy this key now — it will not be shown again">
			<div class="flex items-center gap-2">
				<code
					class="flex-1 break-all rounded-md bg-[var(--color-fg)] px-3 py-2 font-mono text-sm text-[var(--color-accent)]"
				>
					{revealedKey}
				</code>
				<Button variant="secondary" onclick={() => copyToClipboard(revealedKey!)}>
					<Copy class="h-4 w-4" />
					Copy
				</Button>
				<Button variant="ghost" onclick={() => (revealedKey = null)}>Dismiss</Button>
			</div>
		</Card>
	{/if}

	<Card title="Issue new key">
		<form onsubmit={handleCreate} class="grid gap-3 sm:grid-cols-[3fr_auto]">
			<Input placeholder="Name (e.g. ci-deploy)" required bind:value={newName} />
			<Button type="submit" disabled={creating}>
				<Plus class="h-4 w-4" />
				{creating ? 'Issuing…' : 'Issue key'}
			</Button>
		</form>
		{#if createError}
			<div class="mt-3 text-sm text-[var(--color-danger)]">{createError}</div>
		{/if}
	</Card>

	{#if loading}
		<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
	{:else if listError}
		<div class="text-sm text-[var(--color-danger)]">{listError}</div>
	{:else if keys.length === 0}
		<EmptyState
			icon={KeyRound}
			title="No keys yet"
			description="Issue one above for CI scripts or external tooling."
		/>
	{:else}
		<Card title="Existing keys" padded={false}>
			<table class="w-full text-sm">
				<thead
					class="text-left text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
				>
					<tr class="border-b border-[var(--color-border)]">
						<th class="px-5 py-2.5">Name</th>
						<th class="px-5 py-2.5">Last used</th>
						<th class="px-5 py-2.5">Created</th>
						<th class="px-5 py-2.5">Status</th>
						<th class="px-5 py-2.5"></th>
					</tr>
				</thead>
				<tbody>
					{#each keys as key}
						<tr
							class="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-bg-subtle)]"
						>
							<td class="px-5 py-3 font-medium text-[var(--color-fg)]">{key.name}</td>
							<td class="px-5 py-3 text-xs text-[var(--color-fg-muted)]">
								{key.lastUsedAt ? new Date(key.lastUsedAt).toLocaleString() : 'Never'}
							</td>
							<td class="px-5 py-3 text-xs text-[var(--color-fg-muted)]">
								{new Date(key.createdAt).toLocaleDateString()}
							</td>
							<td class="px-5 py-3">
								{#if key.revokedAt}
									<Badge tone="neutral" size="sm">revoked</Badge>
								{:else}
									<Badge tone="success" size="sm">active</Badge>
								{/if}
							</td>
							<td class="px-5 py-3 text-right">
								{#if !key.revokedAt}
									<button
										class="text-xs font-medium text-[var(--color-danger)] hover:underline"
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
		</Card>
	{/if}
</div>
