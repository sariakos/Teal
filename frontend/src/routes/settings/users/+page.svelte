<script lang="ts">
	import { onMount } from 'svelte';
	import { usersApi } from '$lib/api/users';
	import { auth } from '$lib/stores/auth.svelte';
	import { ApiError } from '$lib/api/client';
	import type { User, UserRole } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';
	import Select from '$lib/components/Select.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { Users, UserPlus, Trash2 } from '@lucide/svelte';

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
			const e = formEmail;
			formEmail = '';
			formPassword = '';
			formRole = 'viewer';
			await reload();
			toast.success(`Invited ${e}`);
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Create failed';
		} finally {
			submitting = false;
		}
	}

	async function handleDelete(user: User) {
		if (auth.user?.id === user.id) return; // backend also rejects this
		if (
			!(await dialog.confirm({
				title: `Delete ${user.email}?`,
				body: 'They will lose access immediately. Active sessions are revoked on next request.',
				tone: 'danger'
			}))
		)
			return;
		try {
			await usersApi.delete(user.id);
			await reload();
			toast.success(`Deleted ${user.email}`);
		} catch (err) {
			toast.error('Delete failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	async function handleRoleChange(user: User, role: UserRole) {
		try {
			await usersApi.update(user.id, { role });
			await reload();
			toast.success(`${user.email} is now ${role}`);
		} catch (err) {
			toast.error('Update failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}
</script>

<div class="space-y-6">
	<PageHeader
		title="Users"
		description="Admins can invite, change roles, and revoke access."
	/>

	<Card title="Invite user">
		<form onsubmit={handleCreate} class="grid gap-3 sm:grid-cols-[2fr_2fr_1fr_auto]">
			<Input type="email" placeholder="email@example.com" required bind:value={formEmail} />
			<Input
				type="password"
				placeholder="Initial password (≥ 12 chars)"
				required
				bind:value={formPassword}
			/>
			<Select bind:value={formRole}>
				<option value="viewer">viewer</option>
				<option value="member">member</option>
				<option value="admin">admin</option>
			</Select>
			<Button type="submit" disabled={submitting}>
				<UserPlus class="h-4 w-4" />
				{submitting ? 'Adding…' : 'Add'}
			</Button>
		</form>
		{#if formError}
			<div class="mt-3 text-sm text-[var(--color-danger)]">{formError}</div>
		{/if}
	</Card>

	{#if loading}
		<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
	{:else if listError}
		<div class="text-sm text-[var(--color-danger)]">{listError}</div>
	{:else if users.length === 0}
		<EmptyState icon={Users} title="No users yet" description="Invite the first user above." />
	{:else}
		<Card title="Existing users" padded={false}>
			<table class="w-full text-sm">
				<thead
					class="text-left text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
				>
					<tr class="border-b border-[var(--color-border)]">
						<th class="px-5 py-2.5">Email</th>
						<th class="px-5 py-2.5">Role</th>
						<th class="px-5 py-2.5">Created</th>
						<th class="px-5 py-2.5"></th>
					</tr>
				</thead>
				<tbody>
					{#each users as user}
						<tr
							class="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-bg-subtle)]"
						>
							<td class="px-5 py-3">
								<span class="font-medium text-[var(--color-fg)]">{user.email}</span>
								{#if auth.user?.id === user.id}
									<Badge tone="accent" size="sm" class="ml-2">you</Badge>
								{/if}
							</td>
							<td class="px-5 py-3">
								<Select
									size="sm"
									value={user.role}
									onchange={(e) =>
										handleRoleChange(
											user,
											(e.currentTarget as HTMLSelectElement).value as UserRole
										)}
								>
									<option value="viewer">viewer</option>
									<option value="member">member</option>
									<option value="admin">admin</option>
								</Select>
							</td>
							<td class="px-5 py-3 text-xs text-[var(--color-fg-muted)]">
								{new Date(user.createdAt).toLocaleDateString()}
							</td>
							<td class="px-5 py-3 text-right">
								{#if auth.user?.id !== user.id}
									<button
										class="inline-flex items-center gap-1 text-xs font-medium text-[var(--color-danger)] hover:underline"
										onclick={() => handleDelete(user)}
									>
										<Trash2 class="h-3.5 w-3.5" />
										Delete
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
