<script lang="ts">
	import { goto } from '$app/navigation';
	import { login } from '$lib/api/auth';
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
		submitting = true;
		try {
			const me = await login(email, password);
			auth.set(me.user);
			goto('/');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Login failed';
		} finally {
			submitting = false;
		}
	}
</script>

<Card title="Sign in to Teal">
	<form onsubmit={handleSubmit} class="space-y-4">
		<div>
			<label for="email" class="mb-1 block text-sm font-medium text-zinc-700">Email</label>
			<Input id="email" type="email" autocomplete="email" required bind:value={email} />
		</div>
		<div>
			<label for="password" class="mb-1 block text-sm font-medium text-zinc-700">Password</label>
			<Input
				id="password"
				type="password"
				autocomplete="current-password"
				required
				bind:value={password}
			/>
		</div>
		{#if error}
			<div class="text-sm text-red-600">{error}</div>
		{/if}
		<Button type="submit" disabled={submitting} class="w-full">
			{submitting ? 'Signing in…' : 'Sign in'}
		</Button>
	</form>
</Card>
