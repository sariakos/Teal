<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bootstrapAdmin, fetchSetupStatus } from '$lib/api/auth';
	import { auth } from '$lib/stores/auth.svelte';
	import { ApiError } from '$lib/api/client';
	import Card from '$lib/components/Card.svelte';
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
		// Pull token from URL first so the user sees a pre-filled field
		// and doesn't have to re-paste from the installer's output.
		const urlToken = page.url.searchParams.get('token');
		if (urlToken) token = urlToken;
		try {
			const status = await fetchSetupStatus();
			requiresToken = !!status.requiresToken;
		} catch {
			// Non-fatal — fall back to "show the field" if we can't tell.
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
			error = 'Bootstrap token is required. Find it in the installer output, or re-run install.sh to get a new one.';
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

<Card title="Create the first admin">
	<p class="mb-4 text-sm text-zinc-600">
		Welcome to Teal. This is a fresh install — set up the admin account that will manage every
		app on this host.
	</p>
	<form onsubmit={handleSubmit} class="space-y-4">
		<div>
			<label for="email" class="mb-1 block text-sm font-medium text-zinc-700">Email</label>
			<Input id="email" type="email" autocomplete="email" required bind:value={email} />
		</div>
		<div>
			<label for="password" class="mb-1 block text-sm font-medium text-zinc-700">
				Password (at least 12 characters)
			</label>
			<Input
				id="password"
				type="password"
				autocomplete="new-password"
				required
				bind:value={password}
			/>
		</div>
		{#if requiresToken}
			<div>
				<label for="token" class="mb-1 block text-sm font-medium text-zinc-700">
					Bootstrap token
				</label>
				<Input id="token" required bind:value={token} placeholder="64-char hex from install.sh" />
				<p class="mt-1 text-xs text-zinc-500">
					Printed by the installer at the end of its output. The token expires the moment the
					first admin is created.
				</p>
			</div>
		{/if}
		{#if error}
			<div class="text-sm text-red-600">{error}</div>
		{/if}
		<Button type="submit" disabled={submitting} class="w-full">
			{submitting ? 'Creating…' : 'Create admin and sign in'}
		</Button>
	</form>
</Card>
