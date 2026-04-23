<script lang="ts">
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import { logout } from '$lib/api/auth';
	import Bell from './Bell.svelte';

	async function handleLogout() {
		try {
			await logout();
		} finally {
			auth.clear();
			goto('/login');
		}
	}
</script>

<header class="flex h-12 items-center justify-between border-b border-zinc-200 bg-white px-5">
	<div class="text-sm text-zinc-500">
		{#if auth.user}
			Signed in as <span class="font-medium text-zinc-800">{auth.user.email}</span>
			<span class="ml-2 text-xs text-zinc-400">({auth.user.role})</span>
		{/if}
	</div>
	{#if auth.user}
		<div class="flex items-center gap-3">
			<Bell />
			<button
				class="text-sm text-zinc-500 hover:text-zinc-800"
				onclick={handleLogout}
			>
				Sign out
			</button>
		</div>
	{/if}
</header>
