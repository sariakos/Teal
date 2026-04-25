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
	<div class="mb-6 flex items-center gap-2">
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
		<h1 class="text-lg font-semibold text-[var(--color-fg)]">Sign in to Teal</h1>
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
