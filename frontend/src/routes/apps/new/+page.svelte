<script lang="ts">
	import { goto } from '$app/navigation';
	import { appsApi } from '$lib/api/apps';
	import { ApiError } from '$lib/api/client';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';

	let name = $state('');
	let slug = $state('');
	let domains = $state(''); // comma-separated in the form
	let branch = $state('main');
	let composeFile = $state(`services:
  web:
    image: nginx:alpine
    ports:
      - "80"
`);
	let error = $state<string | null>(null);
	let submitting = $state(false);

	// Auto-derive slug from name as the user types, but only if the user
	// hasn't manually edited the slug field yet.
	let slugTouched = $state(false);
	$effect(() => {
		if (!slugTouched) {
			slug = name
				.toLowerCase()
				.replace(/[^a-z0-9]+/g, '-')
				.replace(/^-+|-+$/g, '')
				.slice(0, 40);
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		submitting = true;
		try {
			const created = await appsApi.create({
				slug,
				name,
				composeFile,
				domains: domains.split(',').map((d) => d.trim()).filter(Boolean),
				autoDeployBranch: branch,
				autoDeployEnabled: false
			});
			goto(`/apps/${created.slug}`);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Create failed';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="mx-auto max-w-3xl space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">New app</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Define a docker-compose app. Teal will inject Traefik routing automatically — you don't need
			to add labels or networks to the file.
		</p>
	</div>

	<Card>
		<form onsubmit={handleSubmit} class="space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div>
					<label for="name" class="mb-1 block text-sm font-medium text-zinc-700">Name</label>
					<Input id="name" required bind:value={name} placeholder="My App" />
				</div>
				<div>
					<label for="slug" class="mb-1 block text-sm font-medium text-zinc-700">
						Slug (used in compose project name)
					</label>
					<input
						id="slug"
						required
						bind:value={slug}
						oninput={() => (slugTouched = true)}
						placeholder="my-app"
						class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-sm focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500"
					/>
				</div>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div>
					<label for="domains" class="mb-1 block text-sm font-medium text-zinc-700">
						Domains (comma-separated; optional)
					</label>
					<Input id="domains" bind:value={domains} placeholder="myapp.local, myapp.example.com" />
				</div>
				<div>
					<label for="branch" class="mb-1 block text-sm font-medium text-zinc-700">
						Auto-deploy branch (for future GitHub integration)
					</label>
					<Input id="branch" bind:value={branch} placeholder="main" />
				</div>
			</div>

			<div>
				<label for="compose" class="mb-1 block text-sm font-medium text-zinc-700">
					docker-compose.yml
				</label>
				<textarea
					id="compose"
					rows="12"
					bind:value={composeFile}
					class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500"
				></textarea>
				<p class="mt-1 text-xs text-zinc-500">
					Add <code>ports:</code> on the service you want routed, or label it <code
						>teal.primary: "true"</code
					>.
				</p>
			</div>

			{#if error}
				<div class="text-sm text-red-600">{error}</div>
			{/if}
			<div class="flex justify-end">
				<Button type="submit" disabled={submitting}>{submitting ? 'Creating…' : 'Create app'}</Button>
			</div>
		</form>
	</Card>
</div>

