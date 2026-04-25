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
	import Badge from '$lib/components/Badge.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import GithubMark from '$lib/components/GithubMark.svelte';
	import { ArrowRight, ExternalLink, ChevronDown, ChevronRight } from '@lucide/svelte';

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
	<PageHeader
		title="GitHub App"
		description="Configure the platform-wide GitHub App. Apps installed on it deploy without deploy keys or PATs. Admin only."
	/>

	{#if createdSlug}
		<Card title="App created — install it on a repo next">
			<p class="text-sm text-[var(--color-fg)]">
				The platform GitHub App
				<code class="rounded bg-[var(--color-bg-subtle)] px-1.5 py-0.5 font-mono">
					{createdSlug}
				</code>
				was created and Teal stored its credentials. Open the install page on GitHub, pick which
				repositories Teal should access, and you're ready to deploy.
			</p>
			<div class="mt-4 flex justify-end gap-2">
				<Button variant="ghost" onclick={() => (createdSlug = null)}>Dismiss</Button>
				<Button
					onclick={() =>
						window.open(
							`https://github.com/apps/${createdSlug}/installations/new`,
							'_blank',
							'noopener'
						)}
				>
					<GithubMark class="h-4 w-4" />
					Install on a repo
					<ArrowRight class="h-4 w-4" />
				</Button>
			</div>
		</Card>
	{/if}

	{#if cfg && cfg.appId > 0 && cfg.hasPrivateKey}
		<Card title="App is configured">
			{#snippet actions()}
				<Badge tone="success">connected</Badge>
			{/snippet}
			<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
				<dt class="text-[var(--color-fg-muted)]">App ID</dt>
				<dd class="font-mono text-[var(--color-fg)]">{cfg.appId}</dd>
				<dt class="text-[var(--color-fg-muted)]">Slug</dt>
				<dd class="font-mono text-[var(--color-fg)]">{cfg.appSlug || '—'}</dd>
				<dt class="text-[var(--color-fg-muted)]">Private key</dt>
				<dd>
					{#if cfg.hasPrivateKey}
						<Badge tone="success" size="sm">stored</Badge>
					{:else}—{/if}
				</dd>
				<dt class="text-[var(--color-fg-muted)]">Webhook secret</dt>
				<dd>
					{#if cfg.hasWebhookSecret}
						<Badge tone="success" size="sm">stored</Badge>
					{:else}—{/if}
				</dd>
			</dl>
			{#if cfg.appSlug}
				<div class="mt-4">
					<Button
						variant="secondary"
						size="sm"
						onclick={() =>
							window.open(
								`https://github.com/apps/${cfg!.appSlug}/installations/new`,
								'_blank',
								'noopener'
							)}
					>
						<ExternalLink class="h-3.5 w-3.5" />
						Install / manage on more repos
					</Button>
				</div>
			{/if}
		</Card>
	{:else}
		<Card
			title="Create the platform GitHub App"
			description="One-click setup: Teal generates a manifest with the right permissions and callback. Confirm on GitHub, no copy-pasting."
		>
			<form
				class="space-y-3"
				onsubmit={(e) => {
					e.preventDefault();
					void startManifestFlow();
				}}
			>
				<div>
					<label for="org" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
						Create under organization
						<span class="text-[var(--color-fg-subtle)]">(optional)</span>
					</label>
					<Input
						id="org"
						bind:value={orgSlug}
						placeholder="leave empty to create under your user account"
					/>
					<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
						If set, you must be an admin of the organization. Otherwise GitHub will create the
						App under your personal account.
					</p>
				</div>
				{#if manifestError}
					<div class="text-sm text-[var(--color-danger)]">{manifestError}</div>
				{/if}
				<div class="flex justify-end">
					<Button type="submit" disabled={manifestSubmitting}>
						<GithubMark class="h-4 w-4" />
						{manifestSubmitting ? 'Redirecting to GitHub…' : 'Create with one click'}
						<ArrowRight class="h-4 w-4" />
					</Button>
				</div>
			</form>
		</Card>
	{/if}

	<div>
		<button
			type="button"
			class="inline-flex items-center gap-1 text-xs text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
			onclick={() => (showManual = !showManual)}
		>
			{#if showManual}
				<ChevronDown class="h-3 w-3" />
			{:else}
				<ChevronRight class="h-3 w-3" />
			{/if}
			Manual setup (re-use an existing App, or for offline installs)
		</button>
	</div>

	{#if showManual}
		<Card title="Manual setup steps (one-time)">
			<ol class="list-decimal space-y-2 pl-5 text-sm text-[var(--color-fg)]">
				<li>
					Go to
					<a
						class="text-[var(--color-accent)] underline"
						target="_blank"
						rel="noopener"
						href="https://github.com/settings/apps/new"
					>
						github.com/settings/apps/new
					</a>
					and fill in the App's settings.
				</li>
				<li>
					Set the <strong>Webhook URL</strong> to your Teal hostname +
					<code>/api/v1/webhooks/github-app</code>, generate a webhook secret with
					<code>openssl rand -hex 32</code>.
				</li>
				<li>
					Set permissions: <strong>Contents: Read</strong>,
					<strong>Metadata: Read</strong>. Subscribe to the <strong>Push</strong> event.
				</li>
				<li>
					Copy <strong>App ID</strong>, <strong>App slug</strong>, and download a private key PEM.
					Paste all three below.
				</li>
			</ol>
		</Card>

		<Card title="Credentials">
			{#if loading}
				<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
			{:else}
				<form
					class="space-y-4"
					onsubmit={(e) => {
						e.preventDefault();
						void save();
					}}
				>
					<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<div>
							<label for="appid" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
								App ID
							</label>
							<Input id="appid" bind:value={appId} placeholder="123456" mono />
						</div>
						<div>
							<label
								for="appslug"
								class="mb-1 block text-sm font-medium text-[var(--color-fg)]"
							>
								App slug
							</label>
							<Input id="appslug" bind:value={appSlug} placeholder="teal-platform" mono />
						</div>
					</div>
					<div>
						<label for="pem" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Private key PEM
							{#if cfg?.hasPrivateKey}
								<span class="ml-1 text-xs text-[var(--color-fg-subtle)]">
									(stored — paste a new key to rotate)
								</span>
							{/if}
						</label>
						<textarea
							id="pem"
							rows="8"
							bind:value={privateKey}
							placeholder={cfg?.hasPrivateKey
								? '(leave empty to keep existing)'
								: '-----BEGIN RSA PRIVATE KEY-----\n…'}
							class="block w-full rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-fg)] hover:border-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
						></textarea>
					</div>
					<div>
						<label for="whsec" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Webhook secret
							{#if cfg?.hasWebhookSecret}
								<span class="ml-1 text-xs text-[var(--color-fg-subtle)]">
									(stored — paste a new value to rotate)
								</span>
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
						<div class="text-sm text-[var(--color-danger)]">{error}</div>
					{/if}
					{#if saved}
						<div class="text-sm text-[var(--color-success)]">Saved.</div>
					{/if}
					<div class="flex justify-end">
						<Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
					</div>
				</form>
			{/if}
		</Card>
	{/if}
</div>
