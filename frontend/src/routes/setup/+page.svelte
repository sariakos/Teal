<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bootstrapAdmin, fetchSetupStatus } from '$lib/api/auth';
	import { auth } from '$lib/stores/auth.svelte';
	import { ApiError } from '$lib/api/client';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';

	let email = $state('');
	let password = $state('');
	// Bootstrap token: prefilled from ?token= in the URL (the installer
	// prints a URL containing it). User can also paste it manually if
	// they came in via the bare hostname.
	let token = $state('');
	let requiresToken = $state(false);
	let submitting = $state(false);
	let error = $state<string | null>(null);

	onMount(async () => {
		const urlToken = page.url.searchParams.get('token');
		if (urlToken) token = urlToken;
		try {
			const status = await fetchSetupStatus();
			requiresToken = !!status.requiresToken;
		} catch {
			requiresToken = true;
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		if (password.length < 12) {
			error = 'Password must be at least 12 characters.';
			return;
		}
		if (requiresToken && !token.trim()) {
			error =
				'Bootstrap token is required. Find it in the installer output, or re-run install.sh to get a new one.';
			return;
		}
		submitting = true;
		try {
			const me = await bootstrapAdmin(email, password, token.trim());
			auth.set(me.user);
			goto('/');
		} catch (err) {
			error =
				err instanceof ApiError
					? err.status === 409
						? 'An admin already exists. Use sign in instead.'
						: err.status === 401
							? 'Invalid bootstrap token. Re-run install.sh to mint a new one.'
							: err.message
					: 'Bootstrap failed';
		} finally {
			submitting = false;
		}
	}
</script>

<div
	class="rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-8 shadow-[var(--shadow-popover)]"
>
	<div class="mb-2 flex items-center gap-2">
		<span
			class="flex h-8 w-8 items-center justify-center rounded-md bg-[var(--color-accent)] text-[var(--color-accent-fg)] shadow-[var(--shadow-xs)]"
		>
			<svg
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2.5"
				stroke-linecap="round"
				stroke-linejoin="round"
				class="h-4 w-4"
				aria-hidden="true"
			>
				<path d="M4 6h16M9 12h11M4 18h16" />
			</svg>
		</span>
		<h1 class="text-lg font-semibold text-[var(--color-fg)]">Welcome to Teal</h1>
	</div>
	<p class="mb-6 text-sm text-[var(--color-fg-muted)]">
		Fresh install — set up the admin account that will manage every app on this host.
	</p>
	<form onsubmit={handleSubmit} class="space-y-4">
		<div>
			<label for="email" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">Email</label>
			<Input id="email" type="email" autocomplete="email" required bind:value={email} />
		</div>
		<div>
			<label for="password" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
				Password
			</label>
			<Input
				id="password"
				type="password"
				autocomplete="new-password"
				required
				bind:value={password}
			/>
			<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">At least 12 characters.</p>
		</div>
		{#if requiresToken}
			<div>
				<label for="token" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
					Bootstrap token
				</label>
				<Input
					id="token"
					required
					mono
					bind:value={token}
					placeholder="64-char hex from install.sh"
				/>
				<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
					Printed by the installer at the end of its output. Single-use — burned the moment the
					first admin is created.
				</p>
			</div>
		{/if}
		{#if error}
			<div
				class="rounded-md border border-[var(--color-danger-soft)] bg-[var(--color-danger-soft)] px-3 py-2 text-sm text-[var(--color-danger-soft-fg)]"
			>
				{error}
			</div>
		{/if}
		<Button type="submit" disabled={submitting} class="w-full">
			{submitting ? 'Creating…' : 'Create admin and sign in'}
		</Button>
	</form>
</div>
