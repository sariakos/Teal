<script lang="ts">
	import { onMount } from 'svelte';
	import { usersApi } from '$lib/api/users';
	import { auth } from '$lib/stores/auth.svelte';
	import { ApiError } from '$lib/api/client';
	import type { User, UserRole } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';

	let users = $state<User[]>([]);
	let loading = $state(true);
	let listError = $state<string | null>(null);

	let formEmail = $state('');
	let formPassword = $state('');
	let formRole = $state<UserRole>('viewer');
	let formError = $state<string | null>(null);
	let submitting = $state(false);

	async function reload() {
		try {
			users = await usersApi.list();
			listError = null;
		} catch (err) {
			listError = err instanceof Error ? err.message : 'Failed to load users';
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	async function handleCreate(e: SubmitEvent) {
		e.preventDefault();
		formError = null;
		submitting = true;
		try {
			await usersApi.create({ email: formEmail, password: formPassword, role: formRole });
			formEmail = '';
			formPassword = '';
			formRole = 'viewer';
			await reload();
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Create failed';
		} finally {
			submitting = false;
		}
	}

	async function handleDelete(user: User) {
		if (auth.user?.id === user.id) return; // backend also rejects this
		if (!confirm(`Delete ${user.email}? This cannot be undone.`)) return;
		try {
			await usersApi.delete(user.id);
			await reload();
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Delete failed');
		}
	}

	async function handleRoleChange(user: User, role: UserRole) {
		try {
			await usersApi.update(user.id, { role });
			await reload();
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Update failed');
		}
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">Users</h1>
		<p class="mt-1 text-sm text-zinc-500">Admins can invite, change roles, and revoke access.</p>
	</div>

	<Card title="Invite user">
		<form onsubmit={handleCreate} class="grid gap-3 sm:grid-cols-[2fr_2fr_1fr_auto]">
			<Input type="email" placeholder="email@example.com" required bind:value={formEmail} />
			<Input type="password" placeholder="Initial password (≥ 12 chars)" required bind:value={formPassword} />
			<select
				bind:value={formRole}
				class="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500"
			>
				<option value="viewer">viewer</option>
				<option value="member">member</option>
				<option value="admin">admin</option>
			</select>
			<Button type="submit" disabled={submitting}>{submitting ? 'Adding…' : 'Add'}</Button>
		</form>
		{#if formError}
			<div class="mt-3 text-sm text-red-600">{formError}</div>
		{/if}
	</Card>

	<Card title="Existing users">
		{#if loading}
			<div class="text-sm text-zinc-500">Loading…</div>
		{:else if listError}
			<div class="text-sm text-red-600">{listError}</div>
		{:else if users.length === 0}
			<div class="text-sm text-zinc-500">No users yet.</div>
		{:else}
			<table class="w-full text-sm">
				<thead class="text-left text-xs uppercase text-zinc-500">
					<tr>
						<th class="pb-2">Email</th>
						<th class="pb-2">Role</th>
						<th class="pb-2">Created</th>
						<th class="pb-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each users as user}
						<tr class="border-t border-zinc-100">
							<td class="py-2 font-medium text-zinc-800">{user.email}</td>
							<td class="py-2">
								<select
									value={user.role}
									onchange={(e) =>
										handleRoleChange(user, (e.currentTarget as HTMLSelectElement).value as UserRole)}
									class="rounded-md border border-zinc-300 bg-white px-2 py-1 text-xs"
								>
									<option value="viewer">viewer</option>
									<option value="member">member</option>
									<option value="admin">admin</option>
								</select>
							</td>
							<td class="py-2 text-zinc-500">{new Date(user.createdAt).toLocaleDateString()}</td>
							<td class="py-2 text-right">
								{#if auth.user?.id !== user.id}
									<button
										class="text-sm text-red-600 hover:underline"
										onclick={() => handleDelete(user)}
									>
										Delete
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
