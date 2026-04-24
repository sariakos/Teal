<!--
  Admin page for the platform-wide GitHub App credentials. The values
  here are entered once after manually creating the App on github.com
  (link in the page body); per-app installation happens later from
  each app's Settings tab.
-->
<script lang="ts">
	import { onMount } from 'svelte';
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

	const callbackURL = $derived(
		typeof location !== 'undefined' ? `${location.origin}/api/v1/github-app/setup-callback` : ''
	);
	const webhookURL = $derived(
		typeof location !== 'undefined' ? `${location.origin}/api/v1/webhooks/github-app` : ''
	);

	onMount(reload);
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">GitHub App</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Configure the platform-wide GitHub App. Apps installed on this App can be deployed without
			deploy keys or PATs. Admin only.
		</p>
	</div>

	<Card title="Setup steps (one-time)">
		<ol class="list-decimal space-y-2 pl-5 text-sm text-zinc-700">
			<li>
				Go to
				<a class="text-teal-700 underline" target="_blank" rel="noopener" href="https://github.com/settings/apps/new">
					https://github.com/settings/apps/new
				</a>
				and fill in:
			</li>
			<li>
				<strong>Homepage URL</strong>: anything (your Teal URL works).
			</li>
			<li>
				<strong>Setup URL</strong>
				<em>(under "Post installation")</em>:
				<code class="ml-1 break-all rounded bg-zinc-100 px-1 py-0.5 text-xs">{callbackURL}</code>
			</li>
			<li>
				Tick <strong>Redirect on update</strong> right under the Setup URL field — without this,
				GitHub won't bounce the browser back to Teal after install and the installation ID never
				gets captured.
			</li>
			<li>
				<strong>Callback URL</strong> (under "Identifying and authorizing users"): leave empty;
				Teal doesn't use OAuth user-flow.
			</li>
			<li>Tick <strong>Request user authorization (OAuth) during installation</strong>: <em>off</em>.</li>
			<li>
				<strong>Webhook URL</strong>:
				<code class="ml-1 break-all rounded bg-zinc-100 px-1 py-0.5 text-xs">{webhookURL}</code>
			</li>
			<li>
				<strong>Webhook secret</strong>: generate one (e.g. <code>openssl rand -hex 32</code>) and
				paste below.
			</li>
			<li>
				<strong>Repository permissions</strong>: <strong>Contents: Read</strong>,
				<strong>Metadata: Read</strong>.
			</li>
			<li>
				<strong>Subscribe to events</strong>: <strong>Push</strong>.
			</li>
			<li>
				After creating, on the App's page: copy <strong>App ID</strong>, the
				<strong>App slug</strong> (the URL fragment after <code>/apps/</code>), and click
				<strong>Generate a private key</strong> to download a PEM. Paste all three below.
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
</div>
