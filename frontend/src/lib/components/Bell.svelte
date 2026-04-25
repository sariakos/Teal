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
	import { Bell as BellIcon } from '@lucide/svelte';
	import StatusDot from './StatusDot.svelte';

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

	function levelTone(level: string): 'danger' | 'warning' | 'accent' {
		if (level === 'error') return 'danger';
		if (level === 'warn') return 'warning';
		return 'accent';
	}
</script>

<div class="relative">
	<button
		class="relative flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-fg-muted)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-fg)]"
		aria-label="Notifications"
		onclick={() => (open = !open)}
	>
		<BellIcon class="h-4 w-4" />
		{#if unread > 0}
			<span
				class="absolute right-0 top-0 inline-flex min-w-[16px] -translate-y-1 translate-x-1 items-center justify-center rounded-full bg-[var(--color-danger)] px-1 text-[10px] font-medium text-[var(--color-danger-fg)]"
			>
				{unread > 99 ? '99+' : unread}
			</span>
		{/if}
	</button>

	{#if open}
		<div
			class="absolute right-0 z-30 mt-2 w-80 overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-popover)]"
		>
			<div
				class="flex items-center justify-between border-b border-[var(--color-border)] px-3 py-2"
			>
				<span class="text-sm font-semibold text-[var(--color-fg)]">Notifications</span>
				<button
					class="text-xs text-[var(--color-accent)] hover:underline"
					onclick={markAll}
				>
					Mark all read
				</button>
			</div>
			<div class="max-h-96 overflow-auto">
				{#if items.length === 0}
					<div class="px-3 py-8 text-center text-sm text-[var(--color-fg-muted)]">
						No notifications.
					</div>
				{:else}
					{#each items as n (n.id)}
						<div
							class="border-b border-[var(--color-border)] px-3 py-2.5 text-sm last:border-0 {!n.readAt
								? 'bg-[var(--color-bg-subtle)]'
								: ''}"
						>
							<div class="flex items-start justify-between gap-2">
								<div class="min-w-0 flex-1">
									<div class="flex items-center gap-1.5 font-medium text-[var(--color-fg)]">
										<StatusDot tone={levelTone(n.level)} />
										<span class="truncate">{n.title}</span>
									</div>
									{#if n.body}
										<div class="mt-1 text-xs text-[var(--color-fg-muted)]">{n.body}</div>
									{/if}
									<div class="mt-1 text-[11px] text-[var(--color-fg-subtle)]">
										{new Date(n.createdAt).toLocaleString()}
										{#if n.appSlug}
											·
											<button
												class="text-[var(--color-accent)] hover:underline"
												onclick={() => gotoApp(n.appSlug)}
											>
												{n.appSlug}
											</button>
										{/if}
									</div>
								</div>
								{#if !n.readAt}
									<button
										class="text-[11px] text-[var(--color-fg-muted)] hover:text-[var(--color-accent)]"
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
