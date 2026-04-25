<script lang="ts">
	/*
	 * New app form. Two modes: "Connect git repo" (recommended — Teal
	 * clones at deploy time and reads compose from the repo) and "Paste
	 * compose" (advanced/quick — paste a docker-compose.yml directly).
	 *
	 * On submit with git mode, the backend may generate an SSH deploy
	 * key + a webhook secret. Both are returned ONCE in the response;
	 * we hold the user on this page until they've copied them.
	 */
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { appsApi } from '$lib/api/apps';
	import { ApiError } from '$lib/api/client';
	import { githubAppReposApi, type AppReposResponse } from '$lib/api/github_app';
	import type { GitAuthKind } from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';

	type Mode = 'git' | 'compose';
	let mode = $state<Mode>('git');

	let name = $state('');
	let slug = $state('');
	let domains = $state(''); // comma-separated in the form
	let branch = $state('main');
	let autoDeploy = $state(true);

	// Git mode
	let gitUrl = $state('');
	let gitAuthKind = $state<GitAuthKind>('github_app');
	let gitCredential = $state('');
	let gitComposePath = $state('docker-compose.yml');

	// Compose mode
	let composeFile = $state(`services:
  web:
    image: nginx:alpine
    ports:
      - "80"
`);

	let error = $state<string | null>(null);
	let submitting = $state(false);

	// One-shot reveals the user must copy before navigating away.
	let revealedSecret = $state<string | null>(null);
	let revealedPublicKey = $state<string | null>(null);
	let revealedFingerprint = $state<string | null>(null);
	let createdSlug = $state<string | null>(null);

	// GitHub App repo picker. Loaded once on mount; stays null when
	// the platform App isn't configured (we just hide the picker
	// then). Selection encoded as "<installationId>::<full_name>::<defaultBranch>"
	// so a single <select> carries everything we need to prefill.
	let ghaRepos = $state<AppReposResponse | null>(null);
	let ghaReposLoading = $state(true);
	let selectedRepoKey = $state('');

	function pickRepo(value: string) {
		selectedRepoKey = value;
		if (!value) return;
		const [_, fullName, defaultBranch] = value.split('::');
		if (!fullName) return;
		const last = fullName.split('/').pop() ?? fullName;
		// Prefill from the repo name. The user can still edit before
		// submitting; slugTouched stays false unless they actually
		// type into the slug field.
		name = last;
		slugTouched = false;
		gitUrl = `https://github.com/${fullName}.git`;
		if (defaultBranch) branch = defaultBranch;
		gitAuthKind = 'github_app';
	}

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
			let installationId: number | undefined;
			let repoFullName: string | undefined;
			if (gitAuthKind === 'github_app' && selectedRepoKey) {
				const [idStr, fullName] = selectedRepoKey.split('::');
				const idNum = Number(idStr);
				if (Number.isInteger(idNum) && idNum > 0 && fullName) {
					installationId = idNum;
					repoFullName = fullName;
				}
			}
			const payload =
				mode === 'git'
					? {
							slug,
							name,
							composeFile: '', // engine reads from git
							domains: domains.split(',').map((d) => d.trim()).filter(Boolean),
							autoDeployBranch: branch,
							autoDeployEnabled: autoDeploy,
							gitUrl,
							gitAuthKind,
							gitCredential: gitCredential || undefined,
							gitBranch: branch,
							gitComposePath,
							githubAppInstallationId: installationId,
							githubAppRepo: repoFullName
						}
					: {
							slug,
							name,
							composeFile,
							domains: domains.split(',').map((d) => d.trim()).filter(Boolean),
							autoDeployBranch: branch,
							autoDeployEnabled: false
						};
			const resp = await appsApi.create(payload);
			createdSlug = resp.slug;
			if (resp.newPublicKey) {
				revealedPublicKey = resp.newPublicKey;
				revealedFingerprint = resp.newKeyFingerprint ?? '';
			}
			if (resp.newWebhookSecret) {
				revealedSecret = resp.newWebhookSecret;
			}
			// If nothing to reveal, jump straight to the app detail page.
			if (!revealedPublicKey && !revealedSecret) {
				goto(`/apps/${resp.slug}`);
			}
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Create failed';
		} finally {
			submitting = false;
		}
	}

	function copyToClipboard(s: string) {
		void navigator.clipboard.writeText(s).catch(() => {});
	}

	function continueToApp() {
		if (createdSlug) goto(`/apps/${createdSlug}`);
	}

	const webhookURL = $derived(
		createdSlug ? `${location.origin}/api/v1/webhooks/github/${createdSlug}` : ''
	);

	onMount(async () => {
		try {
			ghaRepos = await githubAppReposApi.listGlobal();
		} catch {
			ghaRepos = null;
		} finally {
			ghaReposLoading = false;
		}
	});
</script>

<div class="mx-auto max-w-3xl space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">New app</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Connect a git repo (recommended) — Teal clones it on every deploy and reads the compose file
			straight from the source. Or paste a compose inline if you don't have a repo yet.
		</p>
	</div>

	{#if revealedPublicKey || revealedSecret}
		<!-- Secrets reveal panel: held here until user clicks Continue. -->
		<Card title="Copy these now — they will not be shown again">
			{#if revealedPublicKey}
				<div class="mb-4">
					<p class="mb-2 text-sm font-medium text-zinc-700">SSH deploy key (public)</p>
					<p class="mb-2 text-xs text-zinc-500">
						Paste into GitHub → repo → <strong>Settings → Deploy keys</strong> → Add deploy key.
						Read access is enough.
					</p>
					<div class="flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-50 px-3 py-2 font-mono text-xs text-zinc-800"
							>{revealedPublicKey}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedPublicKey!)}>
							Copy
						</Button>
					</div>
					{#if revealedFingerprint}
						<p class="mt-2 font-mono text-xs text-zinc-500">Fingerprint: {revealedFingerprint}</p>
					{/if}
				</div>
			{/if}
			{#if revealedSecret}
				<div>
					<p class="mb-2 text-sm font-medium text-zinc-700">Webhook secret</p>
					<p class="mb-2 text-xs text-zinc-500">
						In GitHub → <strong>Settings → Webhooks</strong> → Add webhook, set Payload URL to:
					</p>
					<div class="mb-2 flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-50 px-3 py-2 font-mono text-xs text-zinc-800"
							>{webhookURL}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(webhookURL)}>Copy</Button>
					</div>
					<p class="mb-2 text-xs text-zinc-500">Content type <code>application/json</code>. Secret:</p>
					<div class="flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-900 px-3 py-2 font-mono text-sm text-teal-300"
							>{revealedSecret}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedSecret!)}>
							Copy
						</Button>
					</div>
				</div>
			{/if}
			<div class="mt-4 flex justify-end">
				<Button onclick={continueToApp}>I've copied them — continue</Button>
			</div>
		</Card>
	{:else}
		<Card>
			<form onsubmit={handleSubmit} class="space-y-5">
				<!-- SOURCE first. Picking a repo here can mutate name/slug
				     below; nothing visually jumps "above" the input the
				     user just touched. -->
				<div class="flex gap-2 border-b border-zinc-200">
					<button
						type="button"
						class="border-b-2 px-3 pb-2 text-sm {mode === 'git'
							? 'border-teal-600 font-medium text-teal-700'
							: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
						onclick={() => (mode = 'git')}
					>
						Connect git repo
					</button>
					<button
						type="button"
						class="border-b-2 px-3 pb-2 text-sm {mode === 'compose'
							? 'border-teal-600 font-medium text-teal-700'
							: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
						onclick={() => (mode = 'compose')}
					>
						Paste compose
					</button>
				</div>

				{#if mode === 'git'}
					{#if ghaRepos && ghaRepos.configured && ghaRepos.installations.some((i) => i.repos.length > 0)}
						<div class="rounded-md border border-teal-200 bg-teal-50 p-3">
							<label for="repoPick" class="mb-1 block text-sm font-medium text-teal-900">
								Pick a connected repo (recommended)
							</label>
							<select
								id="repoPick"
								value={selectedRepoKey}
								onchange={(e) => pickRepo((e.currentTarget as HTMLSelectElement).value)}
								class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm"
							>
								<option value="">— or fill in manually below —</option>
								{#each ghaRepos.installations as inst (inst.installationId)}
									{#if inst.repos.length > 0}
										<optgroup label={inst.accountLogin}>
											{#each inst.repos as r (r.fullName)}
												<option value={`${inst.installationId}::${r.fullName}::${r.defaultBranch}`}>
													{r.fullName}{r.private ? ' 🔒' : ''}
												</option>
											{/each}
										</optgroup>
									{/if}
								{/each}
							</select>
							<p class="mt-1 text-xs text-teal-800">
								Picking a repo prefills name, slug, branch, git URL and links the GitHub
								App installation in one save.
							</p>
						</div>
					{:else if ghaRepos && !ghaRepos.configured && !ghaReposLoading}
						<div class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
							The platform GitHub App isn't configured yet — set it up at
							<a class="underline" href="/settings/github-app">Settings → GitHub App</a>
							to get a one-click repo picker here. You can still fill in the form manually.
						</div>
					{:else if ghaRepos && ghaRepos.configured && ghaRepos.installations.length === 0}
						<div class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
							The platform GitHub App is configured but isn't installed on any repo yet.
							{#if ghaRepos.appSlug}
								<a
									class="underline"
									target="_blank"
									rel="noopener"
									href={`https://github.com/apps/${ghaRepos.appSlug}/installations/new`}
								>
									Install on GitHub →
								</a>
							{/if}
						</div>
					{/if}
				{/if}

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

				<div>
					<label for="domains" class="mb-1 block text-sm font-medium text-zinc-700">
						Domains (comma-separated; optional)
					</label>
					<Input id="domains" bind:value={domains} placeholder="myapp.srv.example.com" />
				</div>

				{#if mode === 'git'}
					<div class="space-y-4">
						<div>
							<label for="giturl" class="mb-1 block text-sm font-medium text-zinc-700">
								Git URL
							</label>
							<Input
								id="giturl"
								required
								bind:value={gitUrl}
								placeholder="git@github.com:owner/repo.git"
							/>
							<p class="mt-1 text-xs text-zinc-500">
								Use the SSH URL for private repos with deploy keys, or the https URL for PAT / public.
							</p>
						</div>
						<div class="grid grid-cols-2 gap-4">
							<div>
								<label for="branch" class="mb-1 block text-sm font-medium text-zinc-700">
									Branch
								</label>
								<Input id="branch" bind:value={branch} placeholder="main" />
							</div>
							<div>
								<label for="path" class="mb-1 block text-sm font-medium text-zinc-700">
									Compose path in repo
								</label>
								<Input id="path" bind:value={gitComposePath} placeholder="docker-compose.yml" />
							</div>
						</div>
						<div>
							<label for="auth" class="mb-1 block text-sm font-medium text-zinc-700">
								Authentication
							</label>
							<select
								id="auth"
								bind:value={gitAuthKind}
								class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm"
							>
								<option value="github_app">GitHub App (recommended; install on the repo after save)</option>
								<option value="ssh">SSH deploy key (Teal generates a keypair)</option>
								<option value="pat">Personal access token</option>
								<option value="">Public repo (no auth)</option>
							</select>
							<p class="mt-1 text-xs text-zinc-500">
								{#if gitAuthKind === 'github_app'}
									Save the app, then click <strong>Install on a repo</strong> on its Settings tab to grant
									access — short-lived tokens, no key copying. Requires the platform GitHub App to be
									configured at <code>/settings/github-app</code>.
								{:else if gitAuthKind === 'ssh'}
									After save, copy the public key shown and paste it into your GitHub repo → Settings → Deploy keys.
								{:else if gitAuthKind === 'pat'}
									Generate a fine-grained PAT in GitHub with read access to this repo, then paste it below.
								{:else}
									Public repos clone over https without auth — make sure the URL starts with <code>https://</code>.
								{/if}
							</p>
						</div>
						{#if gitAuthKind === 'pat'}
							<div>
								<label for="cred" class="mb-1 block text-sm font-medium text-zinc-700">
									Personal access token
								</label>
								<Input id="cred" type="password" required bind:value={gitCredential} placeholder="ghp_…" />
							</div>
						{/if}
						<label class="flex items-center gap-2 text-sm text-zinc-700">
							<input type="checkbox" bind:checked={autoDeploy} />
							Auto-deploy on push to <code class="ml-1">{branch}</code> (you can configure the GitHub
							webhook later from the app's Settings tab)
						</label>
					</div>
				{:else}
					<div>
						<label for="compose" class="mb-1 block text-sm font-medium text-zinc-700">
							docker-compose.yml
						</label>
						<textarea
							id="compose"
							rows="14"
							bind:value={composeFile}
							class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500"
						></textarea>
						<p class="mt-1 text-xs text-zinc-500">
							Add <code>ports:</code> on the service you want routed, or label it <code
								>teal.primary: "true"</code
							>.
						</p>
					</div>
				{/if}

				{#if error}
					<div class="text-sm text-red-600">{error}</div>
				{/if}
				<div class="flex justify-end">
					<Button type="submit" disabled={submitting}>
						{submitting ? 'Creating…' : 'Create app'}
					</Button>
				</div>
			</form>
		</Card>
	{/if}
</div>
