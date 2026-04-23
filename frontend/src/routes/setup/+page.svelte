<script lang="ts">
	import { goto } from '$app/navigation';
	import { bootstrapAdmin } from '$lib/api/auth';
	import { auth } from '$lib/stores/auth.svelte';
	import { ApiError } from '$lib/api/client';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);
	let error = $state<string | null>(null);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		if (password.length < 12) {
			error = 'Password must be at least 12 characters.';
			return;
		}
		submitting = true;
		try {
			const me = await bootstrapAdmin(email, password);
			auth.set(me.user);
			goto('/');
		} catch (err) {
			error =
				err instanceof ApiError
					? err.status === 409
						? 'An admin already exists. Use sign in instead.'
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
		{#if error}
			<div class="text-sm text-red-600">{error}</div>
		{/if}
		<Button type="submit" disabled={submitting} class="w-full">
			{submitting ? 'Creating…' : 'Create admin and sign in'}
		</Button>
	</form>
</Card>
