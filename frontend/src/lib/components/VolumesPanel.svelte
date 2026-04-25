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
	import Card from './Card.svelte';
	import EmptyState from './EmptyState.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { HardDrive, Trash2 } from '@lucide/svelte';

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

{#if error}
	<Card>
		<div class="text-sm text-[var(--color-danger)]">{error}</div>
	</Card>
{:else if loading}
	<Card>
		<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
	</Card>
{:else if rows.length === 0}
	<EmptyState
		icon={HardDrive}
		title={appSlug ? `No volumes for ${appSlug}` : 'No volumes on this host'}
		description={appSlug
			? 'Compose-declared volumes appear here once the first deploy creates them.'
			: 'Deploy an app with a volume to see it here.'}
	/>
{:else}
	<Card title={appSlug ? `Volumes for ${appSlug}` : 'All volumes'} padded={false}>
		<table class="w-full text-sm">
			<thead
				class="text-left text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
			>
				<tr class="border-b border-[var(--color-border)]">
					<th class="px-5 py-2.5">Name</th>
					<th class="px-5 py-2.5">Driver</th>
					<th class="px-5 py-2.5">Mountpoint</th>
					<th class="px-5 py-2.5">Created</th>
					<th class="px-5 py-2.5"></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as v}
					<tr
						class="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-bg-subtle)]"
					>
						<td class="px-5 py-2.5 font-mono text-xs text-[var(--color-fg)]">{v.name}</td>
						<td class="px-5 py-2.5 text-[var(--color-fg-muted)]">{v.driver}</td>
						<td class="px-5 py-2.5 truncate font-mono text-xs text-[var(--color-fg-muted)]">
							{v.mountpoint}
						</td>
						<td class="px-5 py-2.5 text-xs text-[var(--color-fg-muted)]">
							{v.createdAt ? new Date(v.createdAt).toLocaleString() : '—'}
						</td>
						<td class="px-5 py-2.5 text-right">
							<button
								class="inline-flex items-center gap-1 text-xs font-medium text-[var(--color-danger)] hover:underline"
								onclick={() => deleteVolume(v.name)}
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
