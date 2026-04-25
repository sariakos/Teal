<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { fetchMe, fetchSetupStatus } from '$lib/api/auth';
	import { auth } from '$lib/stores/auth.svelte';
	import { theme } from '$lib/stores/theme.svelte';
	import Sidebar from '$lib/components/Sidebar.svelte';
	import TopBar from '$lib/components/TopBar.svelte';
	import Toaster from '$lib/components/Toaster.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

	let { children } = $props();

	// Pages that don't need (and won't render) the chrome.
	const PUBLIC_ROUTES = ['/login', '/setup'];

	let booted = $state(false);

	onMount(async () => {
		theme.init();

		// Resolve auth state and bootstrap state in parallel; redirect once
		// we know enough to make a decision.
		const [me, setup] = await Promise.all([fetchMe(), fetchSetupStatus().catch(() => null)]);
		auth.set(me?.user ?? null);
		booted = true;

		const path = page.url.pathname;
		if (setup?.noUsersYet) {
			if (path !== '/setup') goto('/setup');
			return;
		}
		if (!me && !PUBLIC_ROUTES.includes(path)) {
			goto('/login');
			return;
		}
		if (me && PUBLIC_ROUTES.includes(path)) {
			goto('/');
		}
	});
</script>

<svelte:head>
	<title>Teal</title>
</svelte:head>

{#if !booted}
	<div class="flex h-screen items-center justify-center text-sm text-[var(--color-fg-muted)]">
		Loading…
	</div>
{:else if PUBLIC_ROUTES.includes(page.url.pathname) || !auth.user}
	<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)] p-4">
		<div class="w-full max-w-md">
			{@render children()}
		</div>
	</div>
{:else}
	<div class="flex h-screen overflow-hidden bg-[var(--color-bg)] text-[var(--color-fg)]">
		<Sidebar />
		<div class="flex flex-1 flex-col overflow-hidden">
			<TopBar />
			<main class="flex-1 overflow-auto">
				<div class="mx-auto max-w-7xl px-6 py-6 lg:px-8">
					{@render children()}
				</div>
			</main>
		</div>
	</div>
{/if}

<Toaster />
<ConfirmDialog />
