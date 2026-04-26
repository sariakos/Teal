<script lang="ts">
	import { goto } from '$app/navigation';
	import { login } from '$lib/api/auth';
	import { auth } from '$lib/stores/auth.svelte';
	import { ApiError } from '$lib/api/client';
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

<div
	class="rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-8 shadow-[var(--shadow-popover)]"
>
	<div class="mb-6">
		<img src="/teal-logo.svg" alt="Teal" class="h-10" />
		<h1 class="mt-3 text-sm font-medium text-[var(--color-fg-muted)]">Sign in</h1>
	</div>
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
				autocomplete="current-password"
				required
				bind:value={password}
			/>
		</div>
		{#if error}
			<div
				class="rounded-md border border-[var(--color-danger-soft)] bg-[var(--color-danger-soft)] px-3 py-2 text-sm text-[var(--color-danger-soft-fg)]"
			>
				{error}
			</div>
		{/if}
		<Button type="submit" disabled={submitting} class="w-full">
			{submitting ? 'Signing in…' : 'Sign in'}
		</Button>
	</form>
</div>
