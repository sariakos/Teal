<script lang="ts">
	/*
	 * Notification bell. Subscribes to the realtime broadcast topic and
	 * the user's per-id topic, polls the list endpoint on open, and
	 * exposes mark-read actions.
	 */
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { subscribe } from '$lib/realtime/socket';
	import {
		notificationsApi,
		type NotificationRow
	} from '$lib/api/notifications';
	import { auth } from '$lib/stores/auth.svelte';

	let items = $state<NotificationRow[]>([]);
	let unread = $state(0);
	let open = $state(false);
	let unsubBroadcast: (() => void) | null = null;
	let unsubUser: (() => void) | null = null;

	async function reload() {
		try {
			const res = await notificationsApi.list(50);
			items = res.items;
			unread = res.unread;
		} catch {
			// Best-effort: leave the previous state.
		}
	}

	function onIncoming() {
		// Cheap path: bump the unread count optimistically; the next
		// reload (on open or interval) will reconcile titles + ids.
		unread = unread + 1;
	}

	async function markRead(id: number) {
		try {
			await notificationsApi.markRead(id);
			items = items.map((n) => (n.id === id ? { ...n, readAt: new Date().toISOString() } : n));
			unread = Math.max(0, unread - 1);
		} catch {
			// ignore
		}
	}

	async function markAll() {
		try {
			await notificationsApi.markAllRead();
			items = items.map((n) => ({ ...n, readAt: n.readAt ?? new Date().toISOString() }));
			unread = 0;
		} catch {
			// ignore
		}
	}

	function gotoApp(slug?: string) {
		if (!slug) return;
		open = false;
		goto(`/apps/${slug}`);
	}

	onMount(() => {
		void reload();
		unsubBroadcast = subscribe('notifications.broadcast', onIncoming);
		if (auth.user) {
			unsubUser = subscribe(`notifications.${auth.user.id}`, onIncoming);
		}
	});

	onDestroy(() => {
		unsubBroadcast?.();
		unsubUser?.();
	});

	$effect(() => {
		if (open) void reload();
	});
</script>

<div class="relative">
	<button
		class="relative rounded-md p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-800"
		aria-label="Notifications"
		onclick={() => (open = !open)}
	>
		<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
			<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
			<path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
		</svg>
		{#if unread > 0}
			<span class="absolute -right-0.5 -top-0.5 inline-flex min-w-[18px] items-center justify-center rounded-full bg-red-600 px-1 text-[10px] font-medium text-white">
				{unread > 99 ? '99+' : unread}
			</span>
		{/if}
	</button>

	{#if open}
		<div
			class="absolute right-0 z-30 mt-2 w-80 rounded-md border border-zinc-200 bg-white shadow-lg"
		>
			<div class="flex items-center justify-between border-b border-zinc-100 px-3 py-2">
				<span class="text-sm font-medium text-zinc-800">Notifications</span>
				<button class="text-xs text-teal-700 hover:underline" onclick={markAll}>
					Mark all read
				</button>
			</div>
			<div class="max-h-96 overflow-auto">
				{#if items.length === 0}
					<div class="px-3 py-6 text-center text-sm text-zinc-500">No notifications.</div>
				{:else}
					{#each items as n (n.id)}
						<div class="border-b border-zinc-100 px-3 py-2 text-sm last:border-0">
							<div class="flex items-start justify-between gap-2">
								<div class="flex-1">
									<div class="font-medium text-zinc-800">
										<span
											class="mr-1 inline-block h-1.5 w-1.5 rounded-full {n.level ===
											'error'
												? 'bg-red-500'
												: n.level === 'warn'
													? 'bg-amber-500'
													: 'bg-teal-500'}"
										></span>
										{n.title}
									</div>
									{#if n.body}
										<div class="mt-1 text-xs text-zinc-600">{n.body}</div>
									{/if}
									<div class="mt-1 text-[11px] text-zinc-400">
										{new Date(n.createdAt).toLocaleString()}
										{#if n.appSlug}
											· <button
												class="text-teal-700 hover:underline"
												onclick={() => gotoApp(n.appSlug)}
											>
												{n.appSlug}
											</button>
										{/if}
									</div>
								</div>
								{#if !n.readAt}
									<button
										class="text-[11px] text-zinc-500 hover:text-teal-700"
										onclick={() => markRead(n.id)}
									>
										Mark read
									</button>
								{/if}
							</div>
						</div>
					{/each}
				{/if}
			</div>
		</div>
	{/if}
</div>
