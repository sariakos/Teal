<script lang="ts">
	/*
	 * Volumes table. With `appSlug` provided, lists only volumes whose name
	 * matches the app's compose project prefix. Without it, lists every
	 * volume on the host (admin /volumes page).
	 *
	 * Delete requires the user to retype the volume name — guards against
	 * misclicks on database volumes.
	 */
	import { onMount } from 'svelte';
	import { ApiError } from '$lib/api/client';
	import { volumesApi } from '$lib/api/volumes';
	import type { DockerVolume } from '$lib/api/types';
	import Button from './Button.svelte';
	import Card from './Card.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';

	let { appSlug }: { appSlug?: string } = $props();

	let rows = $state<DockerVolume[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	async function reload() {
		loading = true;
		error = null;
		try {
			rows = await volumesApi.list(appSlug);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load volumes';
		} finally {
			loading = false;
		}
	}

	async function deleteVolume(name: string) {
		const typed = await dialog.prompt({
			title: 'Delete this volume?',
			body: 'All data inside the volume is destroyed. This is the right move for one-off scratch volumes; think twice for databases.',
			tone: 'danger',
			expect: name,
			placeholder: name,
			help: 'Type the volume name to confirm.',
			confirmLabel: 'Delete volume'
		});
		if (typed !== name) return;
		try {
			await volumesApi.remove(name);
			await reload();
			toast.success(`Volume "${name}" deleted`);
		} catch (err) {
			if (err instanceof ApiError && err.status === 409) {
				toast.error('Volume is in use', {
					description: 'Stop the container that mounts it first, then retry.'
				});
				return;
			}
			toast.error('Delete failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	onMount(reload);
</script>

<Card title={appSlug ? `Volumes for ${appSlug}` : 'Volumes'}>
	{#if error}
		<div class="text-sm text-red-600">{error}</div>
	{:else if loading}
		<div class="text-sm text-zinc-500">Loading…</div>
	{:else if rows.length === 0}
		<p class="text-sm text-zinc-500">
			{appSlug
				? 'No volumes for this app. Compose-declared volumes appear here once the first deploy creates them.'
				: 'No volumes on this host.'}
		</p>
	{:else}
		<table class="w-full text-sm">
			<thead class="text-left text-xs uppercase text-zinc-500">
				<tr>
					<th class="pb-2">Name</th>
					<th class="pb-2">Driver</th>
					<th class="pb-2">Mountpoint</th>
					<th class="pb-2">Created</th>
					<th class="pb-2"></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as v}
					<tr class="border-t border-zinc-100">
						<td class="py-2 font-mono text-xs">{v.name}</td>
						<td class="py-2 text-zinc-600">{v.driver}</td>
						<td class="py-2 font-mono text-xs text-zinc-500">{v.mountpoint}</td>
						<td class="py-2 text-zinc-500">
							{v.createdAt ? new Date(v.createdAt).toLocaleString() : '—'}
						</td>
						<td class="py-2 text-right">
							<Button variant="danger" onclick={() => deleteVolume(v.name)}>Delete</Button>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	{/if}
</Card>
