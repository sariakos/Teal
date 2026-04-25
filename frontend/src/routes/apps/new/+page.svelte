<script lang="ts">
	/*
	 * New app form. Two modes: "Connect git repo" (recommended) and "Paste
	 * compose" (advanced). On submit with git mode, the backend may
	 * generate an SSH deploy key + a webhook secret. Both are returned
	 * ONCE in the response; we hold the user on this page until they've
	 * copied them.
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
	import Select from '$lib/components/Select.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import GithubMark from '$lib/components/GithubMark.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { Copy, ArrowRight, GitBranch, FileCode2 } from '@lucide/svelte';

	type Mode = 'git' | 'compose';
	let mode = $state<Mode>('git');

	let name = $state('');
	let slug = $state('');
	let branch = $state('main');
	let autoDeploy = $state(true);

	let gitUrl = $state('');
	let gitAuthKind = $state<GitAuthKind>('github_app');
	let gitCredential = $state('');
	let gitComposePath = $state('docker-compose.yml');

	let composeFile = $state(`services:
  web:
    image: nginx:alpine
    ports:
      - "80"
`);

	let error = $state<string | null>(null);
	let submitting = $state(false);

	let revealedSecret = $state<string | null>(null);
	let revealedPublicKey = $state<string | null>(null);
	let revealedFingerprint = $state<string | null>(null);
	let createdSlug = $state<string | null>(null);

	let ghaRepos = $state<AppReposResponse | null>(null);
	let ghaReposLoading = $state(true);
	let selectedRepoKey = $state('');

	function pickRepo(value: string) {
		selectedRepoKey = value;
		if (!value) return;
		const [, fullName, defaultBranch] = value.split('::');
		if (!fullName) return;
		const last = fullName.split('/').pop() ?? fullName;
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
							composeFile: '',
							domains: [],
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
							domains: [],
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
			if (!revealedPublicKey && !revealedSecret) {
				toast.success(`App "${resp.slug}" created`);
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
		toast.success('Copied to clipboard', { duration: 1500 });
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

	const modes: { value: Mode; label: string; icon: typeof GitBranch }[] = [
		{ value: 'git', label: 'Connect git repo', icon: GitBranch },
		{ value: 'compose', label: 'Paste compose', icon: FileCode2 }
	];
</script>

<div class="mx-auto max-w-3xl space-y-6">
	<PageHeader
		title="New app"
		description="Connect a git repo (recommended) — Teal clones it on every deploy and reads compose from source. Or paste a compose inline."
	/>

	{#if revealedPublicKey || revealedSecret}
		<Card title="Copy these now — they will not be shown again">
			{#if revealedPublicKey}
				<div class="mb-4">
					<p class="mb-1 text-sm font-semibold text-[var(--color-fg)]">SSH deploy key</p>
					<p class="mb-2 text-xs text-[var(--color-fg-muted)]">
						Paste into GitHub → repo → <strong>Settings → Deploy keys</strong> → Add deploy key.
						Read access is enough.
					</p>
					<div class="flex items-center gap-2">
						<code
							class="flex-1 break-all rounded-md bg-[var(--color-bg-subtle)] px-3 py-2 font-mono text-xs text-[var(--color-fg)]"
						>
							{revealedPublicKey}
						</code>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedPublicKey!)}>
							<Copy class="h-4 w-4" />
							Copy
						</Button>
					</div>
					{#if revealedFingerprint}
						<p class="mt-2 font-mono text-xs text-[var(--color-fg-muted)]">
							Fingerprint: {revealedFingerprint}
						</p>
					{/if}
				</div>
			{/if}
			{#if revealedSecret}
				<div>
					<p class="mb-1 text-sm font-semibold text-[var(--color-fg)]">Webhook secret</p>
					<p class="mb-2 text-xs text-[var(--color-fg-muted)]">
						In GitHub → <strong>Settings → Webhooks</strong> → Add webhook, set Payload URL to:
					</p>
					<div class="mb-3 flex items-center gap-2">
						<code
							class="flex-1 break-all rounded-md bg-[var(--color-bg-subtle)] px-3 py-2 font-mono text-xs text-[var(--color-fg)]"
						>
							{webhookURL}
						</code>
						<Button variant="secondary" onclick={() => copyToClipboard(webhookURL)}>
							<Copy class="h-4 w-4" />
							Copy
						</Button>
					</div>
					<p class="mb-2 text-xs text-[var(--color-fg-muted)]">
						Content type <code>application/json</code>. Secret:
					</p>
					<div class="flex items-center gap-2">
						<code
							class="flex-1 break-all rounded-md bg-[var(--color-fg)] px-3 py-2 font-mono text-sm text-[var(--color-accent)]"
						>
							{revealedSecret}
						</code>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedSecret!)}>
							<Copy class="h-4 w-4" />
							Copy
						</Button>
					</div>
				</div>
			{/if}
			<div class="mt-5 flex justify-end">
				<Button onclick={continueToApp}>
					I've copied them — continue
					<ArrowRight class="h-4 w-4" />
				</Button>
			</div>
		</Card>
	{:else}
		<Card>
			<form onsubmit={handleSubmit} class="space-y-5">
				<!-- SOURCE first. Picking a repo here can mutate name/slug
				     below; nothing visually jumps "above" the input the
				     user just touched. -->
				<div class="flex gap-1 rounded-lg bg-[var(--color-bg-subtle)] p-1">
					{#each modes as m}
						{@const Icon = m.icon}
						<button
							type="button"
							onclick={() => (mode = m.value)}
							class="flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors {mode ===
							m.value
								? 'bg-[var(--color-surface)] font-medium text-[var(--color-fg)] shadow-[var(--shadow-xs)]'
								: 'text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]'}"
						>
							<Icon class="h-4 w-4" />
							{m.label}
						</button>
					{/each}
				</div>

				{#if mode === 'git'}
					{#if ghaRepos && ghaRepos.configured && ghaRepos.installations.some((i) => i.repos.length > 0)}
						<div
							class="rounded-lg border border-[var(--color-accent-soft)] bg-[var(--color-accent-soft)] p-3"
						>
							<label
								for="repoPick"
								class="mb-1 flex items-center gap-1.5 text-sm font-medium text-[var(--color-accent-soft-fg)]"
							>
								<GithubMark class="h-3.5 w-3.5" />
								Pick a connected repo (recommended)
							</label>
							<Select
								id="repoPick"
								value={selectedRepoKey}
								onchange={(e) => pickRepo((e.currentTarget as HTMLSelectElement).value)}
							>
								<option value="">— or fill in manually below —</option>
								{#each ghaRepos.installations as inst (inst.installationId)}
									{#if inst.repos.length > 0}
										<optgroup label={inst.accountLogin}>
											{#each inst.repos as r (r.fullName)}
												<option
													value={`${inst.installationId}::${r.fullName}::${r.defaultBranch}`}
												>
													{r.fullName}{r.private ? ' 🔒' : ''}
												</option>
											{/each}
										</optgroup>
									{/if}
								{/each}
							</Select>
							<p class="mt-2 text-xs text-[var(--color-accent-soft-fg)] opacity-80">
								Picking a repo prefills name, slug, branch, git URL and links the GitHub App
								installation in one save.
							</p>
						</div>
					{:else if ghaRepos && !ghaRepos.configured && !ghaReposLoading}
						<div
							class="rounded-lg border border-[var(--color-warning-soft)] bg-[var(--color-warning-soft)] p-3 text-sm text-[var(--color-warning-soft-fg)]"
						>
							The platform GitHub App isn't configured yet — set it up at
							<a class="underline" href="/settings/github-app">Settings → GitHub App</a>
							to get a one-click repo picker here. You can still fill in the form manually.
						</div>
					{:else if ghaRepos && ghaRepos.configured && ghaRepos.installations.length === 0}
						<div
							class="rounded-lg border border-[var(--color-warning-soft)] bg-[var(--color-warning-soft)] p-3 text-sm text-[var(--color-warning-soft-fg)]"
						>
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

				<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
					<div>
						<label for="name" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Name
						</label>
						<Input id="name" required bind:value={name} placeholder="My App" />
					</div>
					<div>
						<label for="slug" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Slug
						</label>
						<input
							id="slug"
							required
							bind:value={slug}
							oninput={() => (slugTouched = true)}
							placeholder="my-app"
							class="block w-full rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] px-3 py-2 font-mono text-sm text-[var(--color-fg)] hover:border-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
						/>
						<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
							Used in the compose project name and URLs.
						</p>
					</div>
				</div>

				{#if mode === 'git'}
					<div class="space-y-4">
						<div>
							<label for="giturl" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
								Git URL
							</label>
							<Input
								id="giturl"
								required
								bind:value={gitUrl}
								placeholder="git@github.com:owner/repo.git"
							/>
							<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
								Use SSH for private repos with deploy keys, or https for PAT / public.
							</p>
						</div>
						<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
							<div>
								<label
									for="branch"
									class="mb-1 block text-sm font-medium text-[var(--color-fg)]"
								>
									Branch
								</label>
								<Input id="branch" bind:value={branch} placeholder="main" />
							</div>
							<div>
								<label for="path" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
									Compose path in repo
								</label>
								<Input
									id="path"
									bind:value={gitComposePath}
									placeholder="docker-compose.yml"
									mono
								/>
							</div>
						</div>
						<div>
							<label for="auth" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
								Authentication
							</label>
							<Select id="auth" bind:value={gitAuthKind}>
								<option value="github_app">GitHub App (recommended)</option>
								<option value="ssh">SSH deploy key (Teal generates a keypair)</option>
								<option value="pat">Personal access token</option>
								<option value="">Public repo (no auth)</option>
							</Select>
							<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
								{#if gitAuthKind === 'github_app'}
									Save the app, then click <strong>Install on a repo</strong> on its Settings tab to
									grant access — short-lived tokens, no key copying. Requires the platform GitHub App to
									be configured at <code>/settings/github-app</code>.
								{:else if gitAuthKind === 'ssh'}
									After save, copy the public key shown and paste it into your GitHub repo →
									Settings → Deploy keys.
								{:else if gitAuthKind === 'pat'}
									Generate a fine-grained PAT in GitHub with read access to this repo, then paste
									it below.
								{:else}
									Public repos clone over https without auth — make sure the URL starts with
									<code>https://</code>.
								{/if}
							</p>
						</div>
						{#if gitAuthKind === 'pat'}
							<div>
								<label for="cred" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
									Personal access token
								</label>
								<Input
									id="cred"
									type="password"
									required
									bind:value={gitCredential}
									placeholder="ghp_…"
								/>
							</div>
						{/if}
						<label class="flex items-center gap-2 text-sm text-[var(--color-fg)]">
							<input
								type="checkbox"
								bind:checked={autoDeploy}
								class="h-4 w-4 rounded border-[var(--color-border-strong)] text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
							/>
							Auto-deploy on push to <code class="ml-1">{branch}</code>
						</label>
					</div>
				{:else}
					<div>
						<label for="compose" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							docker-compose.yml
						</label>
						<textarea
							id="compose"
							rows="14"
							bind:value={composeFile}
							class="block w-full rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-fg)] hover:border-[var(--color-fg-subtle)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
						></textarea>
						<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
							Add <code>ports:</code> on the service you want routed, or label it
							<code>teal.primary: "true"</code>.
						</p>
					</div>
				{/if}

				{#if error}
					<div
						class="rounded-md border border-[var(--color-danger-soft)] bg-[var(--color-danger-soft)] px-3 py-2 text-sm text-[var(--color-danger-soft-fg)]"
					>
						{error}
					</div>
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
