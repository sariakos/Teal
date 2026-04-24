<script lang="ts">
	import { page } from '$app/state';

	interface NavItem {
		href: string;
		label: string;
	}

	const items: NavItem[] = [
		{ href: '/', label: 'Dashboard' },
		{ href: '/apps/new', label: 'New App' },
		{ href: '/volumes', label: 'Volumes' },
		{ href: '/settings/users', label: 'Users' },
		{ href: '/settings/apikeys', label: 'API Keys' },
		{ href: '/settings/shared-env', label: 'Shared env' },
		{ href: '/settings/platform', label: 'Platform' },
		{ href: '/settings/github-app', label: 'GitHub App' }
	];

	function isActive(href: string): boolean {
		if (href === '/') return page.url.pathname === '/';
		return page.url.pathname.startsWith(href);
	}
</script>

<aside class="w-56 shrink-0 border-r border-zinc-200 bg-white">
	<div class="px-5 py-4 text-lg font-semibold text-teal-700">Teal</div>
	<nav class="px-2 py-2">
		{#each items as item}
			<a
				href={item.href}
				class="block rounded-md px-3 py-2 text-sm text-zinc-700 hover:bg-zinc-100 {isActive(
					item.href
				)
					? 'bg-teal-50 font-medium text-teal-700'
					: ''}"
			>
				{item.label}
			</a>
		{/each}
	</nav>
</aside>
