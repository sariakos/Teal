<script lang="ts">
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import { logout } from '$lib/api/auth';
	import { theme } from '$lib/stores/theme.svelte';
	import Bell from './Bell.svelte';
	import { Sun, Moon } from '@lucide/svelte';

	async function handleLogout() {
		try {
			await logout();
		} finally {
			auth.clear();
			goto('/login');
		}
	}
</script>

<header
	class="flex h-12 items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-surface)] px-5"
>
	<div class="text-sm text-[var(--color-fg-muted)]">
		{#if auth.user}
			Signed in as <span class="font-medium text-[var(--color-fg)]">{auth.user.email}</span>
			<span class="ml-2 text-xs text-[var(--color-fg-subtle)]">({auth.user.role})</span>
		{/if}
	</div>
	{#if auth.user}
		<div class="flex items-center gap-2">
			<button
				class="flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-fg-muted)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-fg)]"
				onclick={() => theme.toggle()}
				title={theme.resolved === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
				aria-label="Toggle theme"
			>
				{#if theme.resolved === 'dark'}
					<Sun class="h-4 w-4" />
				{:else}
					<Moon class="h-4 w-4" />
				{/if}
			</button>
			<Bell />
			<button
				class="ml-1 text-sm text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
				onclick={handleLogout}
			>
				Sign out
			</button>
		</div>
	{/if}
</header>
