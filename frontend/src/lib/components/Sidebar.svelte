<script lang="ts">
	import { page } from '$app/state';
	import {
		LayoutDashboard,
		PlusCircle,
		HardDrive,
		Users,
		KeyRound,
		Globe,
		Server
	} from '@lucide/svelte';
	import GithubMark from './GithubMark.svelte';
	import type { Component } from 'svelte';

	interface NavItem {
		href: string;
		label: string;
		icon: Component;
	}

	interface NavGroup {
		label?: string;
		items: NavItem[];
	}

	const groups: NavGroup[] = [
		{
			items: [
				{ href: '/', label: 'Dashboard', icon: LayoutDashboard },
				{ href: '/apps/new', label: 'New app', icon: PlusCircle },
				{ href: '/volumes', label: 'Volumes', icon: HardDrive }
			]
		},
		{
			label: 'Settings',
			items: [
				{ href: '/settings/users', label: 'Users', icon: Users },
				{ href: '/settings/apikeys', label: 'API keys', icon: KeyRound },
				{ href: '/settings/shared-env', label: 'Shared env', icon: Globe },
				{ href: '/settings/platform', label: 'Platform', icon: Server },
				{ href: '/settings/github-app', label: 'GitHub App', icon: GithubMark }
			]
		}
	];

	function isActive(href: string): boolean {
		if (href === '/') return page.url.pathname === '/';
		if (href === '/apps/new') return page.url.pathname === '/apps/new';
		return page.url.pathname.startsWith(href);
	}
</script>

<aside
	class="flex w-60 shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-surface)]"
>
	<a
		href="/"
		class="flex items-center gap-2 px-5 py-4 text-base font-semibold text-[var(--color-fg)] no-underline hover:no-underline"
	>
		<img src="/favicon.svg" alt="" class="h-7 w-7" />
		Teal
	</a>
	<nav class="flex-1 space-y-5 overflow-y-auto px-3 pb-6">
		{#each groups as g}
			<div>
				{#if g.label}
					<div
						class="px-3 pb-1.5 text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
					>
						{g.label}
					</div>
				{/if}
				<div class="space-y-0.5">
					{#each g.items as item}
						{@const active = isActive(item.href)}
						{@const Icon = item.icon}
						<a
							href={item.href}
							class="flex items-center gap-2.5 rounded-md px-3 py-1.5 text-sm transition-colors no-underline hover:no-underline {active
								? 'bg-[var(--color-accent-soft)] font-medium text-[var(--color-accent-soft-fg)]'
								: 'text-[var(--color-fg-muted)] hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-fg)]'}"
						>
							<Icon class="h-4 w-4 shrink-0" />
							<span>{item.label}</span>
						</a>
					{/each}
				</div>
			</div>
		{/each}
	</nav>
</aside>
