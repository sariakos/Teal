<!--
  Admin page for the platform-wide GitHub App credentials.

  Two ways to set up:

  1. ONE-CLICK MANIFEST FLOW (recommended). Click the big button →
     browser POSTs a manifest to GitHub → operator clicks "Create" →
     GitHub redirects back, Teal stores everything. No copy-pasting.

  2. MANUAL FORM (fallback). For re-using an existing App, or
     environments where GitHub blocks redirects to the manifest
     callback URL.

  Per-app installation happens later from each app's Settings tab.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api/client';
	import { githubAppApi, type GitHubAppConfig } from '$lib/api/github_app';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';

	let cfg = $state<GitHubAppConfig | null>(null);
	let appId = $state('');
	let appSlug = $state('');
	let privateKey = $state('');
	let webhookSecret = $state('');
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let saved = $state(false);

	// Manifest flow state.
	let orgSlug = $state('');
	let manifestSubmitting = $state(false);
	let manifestError = $state<string | null>(null);
	let createdSlug = $state<string | null>(null);
	// "Show advanced" toggle: collapse the manual form unless the
	// operator opens it. Most users don't need it.
	let showManual = $state(false);

	async function reload() {
		try {
			cfg = await githubAppApi.get();
			appId = cfg.appId > 0 ? String(cfg.appId) : '';
			appSlug = cfg.appSlug;
			// Don't show stored secrets — operator pastes a new value to rotate.
			privateKey = '';
			webhookSecret = '';
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load';
		} finally {
			loading = false;
		}
	}

	async function save() {
		saving = true;
		error = null;
		saved = false;
		try {
			const payload: Parameters<typeof githubAppApi.put>[0] = {};
			const idNum = appId === '' ? 0 : Number(appId);
			if (!Number.isNaN(idNum)) payload.appId = idNum;
			payload.appSlug = appSlug.trim();
			if (privateKey.trim() !== '') payload.privateKeyPem = privateKey;
			if (webhookSecret.trim() !== '') payload.webhookSecret = webhookSecret;
			await githubAppApi.put(payload);
			saved = true;
			await reload();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	// Build a hidden form and submit it to GitHub. We can't redirect
	// to a GET URL because manifest payloads exceed URL length limits;
	// the manifest flow is defined to use a POST form.
	async function startManifestFlow() {
		manifestSubmitting = true;
		manifestError = null;
		try {
			const init = await githubAppApi.manifestInit(orgSlug.trim());
			const form = document.createElement('form');
			form.method = 'POST';
			form.action = init.postUrl;
			form.style.display = 'none';

			const manifestField = document.createElement('input');
			manifestField.type = 'hidden';
			manifestField.name = 'manifest';
			manifestField.value = JSON.stringify(init.manifest);
			form.appendChild(manifestField);

			if (init.state) {
				const stateField = document.createElement('input');
				stateField.type = 'hidden';
				stateField.name = 'state';
				stateField.value = init.state;
				form.appendChild(stateField);
			}

			document.body.appendChild(form);
			form.submit();
		} catch (err) {
			manifestError = err instanceof ApiError ? err.message : 'Could not start manifest flow';
			manifestSubmitting = false;
		}
	}

	onMount(async () => {
		const c = page.url.searchParams.get('created');
		if (c) createdSlug = c;
		await reload();
	});
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">GitHub App</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Configure the platform-wide GitHub App. Apps installed on this App can be deployed without
			deploy keys or PATs. Admin only.
		</p>
	</div>

	{#if createdSlug}
		<Card title="App created — install it on a repo next">
			<p class="text-sm text-zinc-700">
				The platform GitHub App
				<code class="rounded bg-zinc-100 px-1.5 py-0.5">{createdSlug}</code>
				was created and Teal stored its credentials. Open the install page on GitHub, pick which
				repositories Teal should access, and you're ready to deploy.
			</p>
			<div class="mt-3 flex justify-end gap-2">
				<a
					class="inline-flex items-center rounded-md bg-teal-600 px-3 py-2 text-sm font-medium text-white hover:bg-teal-700"
					href={`https://github.com/apps/${createdSlug}/installations/new`}
					target="_blank"
					rel="noopener"
				>
					Install on a repo →
				</a>
				<Button variant="secondary" onclick={() => (createdSlug = null)}>Dismiss</Button>
			</div>
		</Card>
	{/if}

	{#if cfg && cfg.appId > 0 && cfg.hasPrivateKey}
		<Card title="App is configured">
			<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm text-zinc-700">
				<dt class="text-zinc-500">App ID</dt><dd class="font-mono">{cfg.appId}</dd>
				<dt class="text-zinc-500">Slug</dt><dd class="font-mono">{cfg.appSlug || '—'}</dd>
				<dt class="text-zinc-500">Private key</dt><dd>{cfg.hasPrivateKey ? '✓ stored' : '—'}</dd>
				<dt class="text-zinc-500">Webhook secret</dt><dd>{cfg.hasWebhookSecret ? '✓ stored' : '—'}</dd>
			</dl>
			{#if cfg.appSlug}
				<div class="mt-3">
					<a
						class="text-sm text-teal-700 hover:underline"
						href={`https://github.com/apps/${cfg.appSlug}/installations/new`}
						target="_blank"
						rel="noopener"
					>
						Install / manage on more repos →
					</a>
				</div>
			{/if}
		</Card>
	{:else}
		<Card title="Create the platform GitHub App">
			<p class="mb-3 text-sm text-zinc-600">
				One-click setup: Teal generates a GitHub App manifest with the right permissions,
				webhook URL, and post-create callback. Click the button, confirm on GitHub, and you're
				done — no copy-pasting App IDs or private keys.
			</p>
			<form
				class="space-y-3"
				onsubmit={(e) => {
					e.preventDefault();
					void startManifestFlow();
				}}
			>
				<div>
					<label for="org" class="mb-1 block text-sm font-medium text-zinc-700">
						Create under organization
						<span class="text-zinc-400">(optional)</span>
					</label>
					<Input id="org" bind:value={orgSlug} placeholder="leave empty to create under your user account" />
					<p class="mt-1 text-xs text-zinc-500">
						If set, you must be an admin of the organization (or have the right permission).
						Otherwise GitHub will create the App under your personal account.
					</p>
				</div>
				{#if manifestError}
					<div class="text-sm text-red-600">{manifestError}</div>
				{/if}
				<div class="flex justify-end">
					<Button type="submit" disabled={manifestSubmitting}>
						{manifestSubmitting ? 'Redirecting to GitHub…' : 'Create with one click →'}
					</Button>
				</div>
			</form>
		</Card>
	{/if}

	<div>
		<button
			type="button"
			class="text-xs text-zinc-500 underline hover:text-zinc-800"
			onclick={() => (showManual = !showManual)}
		>
			{showManual ? 'Hide' : 'Show'} manual setup (re-using an existing App, or for offline installs)
		</button>
	</div>

	{#if showManual}
		<Card title="Manual setup steps (one-time)">
			<ol class="list-decimal space-y-2 pl-5 text-sm text-zinc-700">
				<li>
					Go to
					<a class="text-teal-700 underline" target="_blank" rel="noopener" href="https://github.com/settings/apps/new">
						https://github.com/settings/apps/new
					</a>
					and fill in the App's settings to match Teal's expectations.
				</li>
				<li>
					Set the <strong>Webhook URL</strong> to your Teal hostname +
					<code>/api/v1/webhooks/github-app</code>, generate a webhook secret with
					<code>openssl rand -hex 32</code>.
				</li>
				<li>
					Set permissions: <strong>Contents: Read</strong>, <strong>Metadata: Read</strong>.
					Subscribe to the <strong>Push</strong> event.
				</li>
				<li>
					After creating, copy <strong>App ID</strong>, <strong>App slug</strong>, and download a
					private key PEM. Paste all three below.
				</li>
			</ol>
		</Card>

		<Card title="Credentials">
			{#if loading}
				<div class="text-sm text-zinc-500">Loading…</div>
			{:else}
				<form
					class="space-y-4"
					onsubmit={(e) => {
						e.preventDefault();
						void save();
					}}
				>
					<div class="grid grid-cols-2 gap-4">
						<div>
							<label for="appid" class="mb-1 block text-sm font-medium text-zinc-700">App ID</label>
							<Input id="appid" bind:value={appId} placeholder="123456" />
						</div>
						<div>
							<label for="appslug" class="mb-1 block text-sm font-medium text-zinc-700">App slug</label>
							<Input id="appslug" bind:value={appSlug} placeholder="teal-platform" />
						</div>
					</div>
					<div>
						<label for="pem" class="mb-1 block text-sm font-medium text-zinc-700">
							Private key PEM
							{#if cfg?.hasPrivateKey}
								<span class="ml-1 text-xs text-zinc-500">(stored — paste a new key to rotate)</span>
							{/if}
						</label>
						<textarea
							id="pem"
							rows="8"
							bind:value={privateKey}
							placeholder={cfg?.hasPrivateKey ? '(leave empty to keep existing)' : '-----BEGIN RSA PRIVATE KEY-----\n…'}
							class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500"
						></textarea>
					</div>
					<div>
						<label for="whsec" class="mb-1 block text-sm font-medium text-zinc-700">
							Webhook secret
							{#if cfg?.hasWebhookSecret}
								<span class="ml-1 text-xs text-zinc-500">(stored — paste a new value to rotate)</span>
							{/if}
						</label>
						<Input
							id="whsec"
							type="password"
							bind:value={webhookSecret}
							placeholder={cfg?.hasWebhookSecret ? '(leave empty to keep existing)' : ''}
						/>
					</div>
					{#if error}
						<div class="text-sm text-red-600">{error}</div>
					{/if}
					{#if saved}
						<div class="text-sm text-teal-700">Saved.</div>
					{/if}
					<div class="flex justify-end">
						<Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
					</div>
				</form>
			{/if}
		</Card>
	{/if}
</div>
