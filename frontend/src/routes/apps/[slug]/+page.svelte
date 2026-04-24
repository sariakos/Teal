<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { appsApi } from '$lib/api/apps';
	import { deploymentsApi } from '$lib/api/deployments';
	import { ApiError } from '$lib/api/client';
	import type {
		AppDetail,
		Deployment,
		DeploymentPhase,
		GitAuthKind
	} from '$lib/api/types';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';
	import EnvVarsPanel from '$lib/components/EnvVarsPanel.svelte';
	import VolumesPanel from '$lib/components/VolumesPanel.svelte';
	import LogsPanel from '$lib/components/LogsPanel.svelte';
	import LogStream from '$lib/components/LogStream.svelte';
	import Sparkline from '$lib/components/Sparkline.svelte';
	import { metricsApi, logsApi, type MetricSample } from '$lib/api/logs';

	const slug = $derived(page.params.slug as string);

	type Tab = 'overview' | 'deployments' | 'logs' | 'env' | 'volumes' | 'settings';
	let tab = $state<Tab>('overview');

	// Overview live-metrics chart state. Re-loaded when the tab is opened
	// or after any metric sample arrives.
	let metrics = $state<MetricSample[]>([]);
	const cpuSeries = $derived(metrics.map((m) => m.cpuPct));
	const memSeries = $derived(metrics.map((m) => m.memBytes / (1024 * 1024)));

	async function loadMetrics() {
		try {
			metrics = await metricsApi.list(slug, '30m');
		} catch {
			metrics = [];
		}
	}

	// Modal: deploy log viewer for a finished deployment.
	let logModalDepID = $state<number | null>(null);
	let logModalText = $state('');
	let logModalLoading = $state(false);
	async function openDeployLog(id: number) {
		logModalDepID = id;
		logModalLoading = true;
		try {
			logModalText = await logsApi.deploymentLog(slug, id);
		} catch (err) {
			logModalText = err instanceof Error ? err.message : 'Failed to load log';
		} finally {
			logModalLoading = false;
		}
	}

	let app = $state<AppDetail | null>(null);
	let appError = $state<string | null>(null);
	let deployments = $state<Deployment[]>([]);

	// In-flight deployment we're polling for live phase updates.
	let watchedID = $state<number | null>(null);
	let watchedPhase = $state<DeploymentPhase>('');
	let watchedStatus = $state<string>('');
	let pollHandle: ReturnType<typeof setInterval> | null = null;

	async function loadApp() {
		try {
			app = await appsApi.get(slug);
			appError = null;
		} catch (err) {
			appError = err instanceof Error ? err.message : 'Failed to load app';
		}
	}
	async function loadDeployments() {
		try {
			deployments = await appsApi.deployments(slug);
		} catch {
			deployments = [];
		}
	}

	function startWatching(id: number) {
		watchedID = id;
		watchedPhase = 'pending';
		watchedStatus = 'pending';
		stopWatching();
		pollHandle = setInterval(async () => {
			try {
				const dep = await deploymentsApi.get(id);
				watchedPhase = dep.phase ?? '';
				watchedStatus = dep.status;
				if (dep.status !== 'pending' && dep.status !== 'running') {
					stopWatching();
					await Promise.all([loadApp(), loadDeployments()]);
				}
			} catch {
				stopWatching();
			}
		}, 1000);
	}

	function stopWatching() {
		if (pollHandle) {
			clearInterval(pollHandle);
			pollHandle = null;
		}
	}

	async function handleDeploy() {
		try {
			const dep = await appsApi.deploy(slug);
			await Promise.all([loadApp(), loadDeployments()]);
			startWatching(dep.id);
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Deploy failed');
		}
	}

	async function handleRollback() {
		if (!confirm('Roll back to the previous successful deployment?')) return;
		try {
			const dep = await appsApi.rollback(slug);
			await Promise.all([loadApp(), loadDeployments()]);
			startWatching(dep.id);
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Rollback failed');
		}
	}

	async function handleDelete() {
		if (!app) return;
		const typed = prompt(`Type the slug "${app.slug}" to confirm permanent deletion.`);
		if (typed !== app.slug) return;
		try {
			await appsApi.delete(slug);
			goto('/');
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Delete failed');
		}
	}

	// ------- Settings tab state -------
	let formGitUrl = $state('');
	let formGitBranch = $state('');
	let formGitComposePath = $state('docker-compose.yml');
	let formGitAuthKind = $state<GitAuthKind>('');
	let formGitCredential = $state('');
	let formAutoDeploy = $state(false);
	let formCPULimit = $state('');
	let formMemoryLimit = $state('');
	let formNotifyWebhookUrl = $state('');
	let formNotifyEmail = $state('');
	let manualInstallationID = $state<string>('');
	let settingsError = $state<string | null>(null);
	let saving = $state(false);

	// Revealed-once values that the UI must show prominently until dismissed.
	let revealedSecret = $state<string | null>(null);
	let revealedPublicKey = $state<string | null>(null);
	let revealedFingerprint = $state<string | null>(null);

	// Existing deploy-key view (loaded on demand in the settings tab).
	let existingPublicKey = $state<string | null>(null);
	let existingFingerprint = $state<string | null>(null);

	function seedSettingsForm(a: AppDetail) {
		formGitUrl = a.gitUrl ?? '';
		formGitBranch = a.gitBranch ?? '';
		formGitComposePath = a.gitComposePath ?? 'docker-compose.yml';
		formGitAuthKind = (a.gitAuthKind ?? '') as GitAuthKind;
		formGitCredential = '';
		formAutoDeploy = a.autoDeployEnabled;
		formCPULimit = a.cpuLimit ?? '';
		formMemoryLimit = a.memoryLimit ?? '';
		formNotifyWebhookUrl = a.notificationWebhookUrl ?? '';
		formNotifyEmail = a.notificationEmail ?? '';
	}

	async function loadExistingDeployKey() {
		if (!app || app.gitAuthKind !== 'ssh' || !app.hasGitCredential) {
			existingPublicKey = null;
			existingFingerprint = null;
			return;
		}
		try {
			const k = await appsApi.getDeployKey(slug);
			existingPublicKey = k.publicKey;
			existingFingerprint = k.fingerprint;
		} catch {
			existingPublicKey = null;
			existingFingerprint = null;
		}
	}

	$effect(() => {
		if (tab === 'settings' && app) {
			seedSettingsForm(app);
			loadExistingDeployKey();
		}
		if (tab === 'overview' && app) {
			void loadMetrics();
		}
	});

	async function saveSettings() {
		if (!app) return;
		saving = true;
		settingsError = null;
		try {
			const resp = await appsApi.update(slug, {
				gitUrl: formGitUrl,
				gitBranch: formGitBranch,
				gitComposePath: formGitComposePath,
				gitAuthKind: formGitAuthKind,
				gitCredential: formGitCredential || undefined,
				autoDeployEnabled: formAutoDeploy,
				cpuLimit: formCPULimit,
				memoryLimit: formMemoryLimit,
				notificationWebhookUrl: formNotifyWebhookUrl,
				notificationEmail: formNotifyEmail
			});
			if (resp.newWebhookSecret) revealedSecret = resp.newWebhookSecret;
			if (resp.newPublicKey) {
				revealedPublicKey = resp.newPublicKey;
				revealedFingerprint = resp.newKeyFingerprint ?? '';
			}
			app = resp;
			formGitCredential = '';
			await loadExistingDeployKey();
		} catch (err) {
			settingsError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function rotateDeployKey() {
		if (!confirm('Rotate the SSH deploy key? You must paste the new public key into GitHub before the next deploy, or it will fail.')) {
			return;
		}
		try {
			const k = await appsApi.rotateDeployKey(slug);
			revealedPublicKey = k.publicKey;
			revealedFingerprint = k.fingerprint;
			await loadApp();
			await loadExistingDeployKey();
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Rotate failed');
		}
	}

	async function rotateWebhookSecret() {
		if (!confirm('Rotate the webhook secret? Update GitHub with the new value afterwards.')) return;
		try {
			const { webhookSecret } = await appsApi.rotateWebhookSecret(slug);
			revealedSecret = webhookSecret;
			await loadApp();
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Rotate failed');
		}
	}

	function copyToClipboard(s: string) {
		void navigator.clipboard.writeText(s).catch(() => {});
	}

	const webhookURL = $derived(
		app ? `${location.origin}/api/v1/webhooks/github/${app.slug}` : ''
	);

	onMount(async () => {
		await Promise.all([loadApp(), loadDeployments()]);
		// If a deployment is currently running, attach the poller.
		const live = deployments.find((d) => d.status === 'running' || d.status === 'pending');
		if (live) startWatching(live.id);
	});

	onDestroy(stopWatching);
</script>

<div class="space-y-6">
	{#if appError}
		<div class="text-sm text-red-600">{appError}</div>
	{:else if !app}
		<div class="text-sm text-zinc-500">Loading…</div>
	{:else}
		<div class="flex items-center justify-between">
			<div>
				<h1 class="text-2xl font-semibold text-zinc-900">{app.name}</h1>
				<p class="mt-1 text-sm text-zinc-500">
					{app.slug} · {app.status}
					{#if app.lastDeployedCommitSha}
						· <span class="font-mono text-xs">{app.lastDeployedCommitSha.slice(0, 7)}</span>
					{/if}
				</p>
			</div>
			<div class="flex gap-2">
				<Button onclick={handleDeploy} disabled={watchedID !== null}>
					{watchedID !== null ? 'Deploying…' : 'Deploy'}
				</Button>
				<Button variant="secondary" onclick={handleRollback} disabled={watchedID !== null}>
					Rollback
				</Button>
				<Button variant="danger" onclick={handleDelete}>Delete</Button>
			</div>
		</div>

		<!-- Tabs -->
		<div class="border-b border-zinc-200">
			<nav class="-mb-px flex gap-6">
				<button
					class="border-b-2 pb-2 text-sm {tab === 'overview'
						? 'border-teal-600 text-teal-700'
						: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
					onclick={() => (tab = 'overview')}
				>
					Overview
				</button>
				<button
					class="border-b-2 pb-2 text-sm {tab === 'deployments'
						? 'border-teal-600 text-teal-700'
						: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
					onclick={() => (tab = 'deployments')}
				>
					Deployments
				</button>
				<button
					class="border-b-2 pb-2 text-sm {tab === 'logs'
						? 'border-teal-600 text-teal-700'
						: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
					onclick={() => (tab = 'logs')}
				>
					Logs
				</button>
				<button
					class="border-b-2 pb-2 text-sm {tab === 'env'
						? 'border-teal-600 text-teal-700'
						: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
					onclick={() => (tab = 'env')}
				>
					Env
				</button>
				<button
					class="border-b-2 pb-2 text-sm {tab === 'volumes'
						? 'border-teal-600 text-teal-700'
						: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
					onclick={() => (tab = 'volumes')}
				>
					Volumes
				</button>
				<button
					class="border-b-2 pb-2 text-sm {tab === 'settings'
						? 'border-teal-600 text-teal-700'
						: 'border-transparent text-zinc-500 hover:text-zinc-800'}"
					onclick={() => (tab = 'settings')}
				>
					Settings
				</button>
			</nav>
		</div>

		{#if tab === 'overview'}
			{#if app.status === 'failed'}
				<div class="rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-800">
					<div class="flex items-center justify-between">
						<div>
							<div class="font-medium">Last deploy failed</div>
							<div class="mt-0.5 text-xs">
								Check the Deployments tab for the captured log; fix and retry.
							</div>
						</div>
						<Button onclick={handleDeploy} disabled={watchedID !== null}>Retry deploy</Button>
					</div>
				</div>
			{/if}
			<div class="grid grid-cols-2 gap-4">
				<Card title="Status">
					<dl class="space-y-2 text-sm">
						<div class="flex justify-between"><dt class="text-zinc-500">App status</dt><dd>{app.status}</dd></div>
						<div class="flex justify-between"><dt class="text-zinc-500">Active color</dt><dd>{app.activeColor || '—'}</dd></div>
						<div class="flex justify-between"><dt class="text-zinc-500">Domains</dt><dd>{app.domains.join(', ') || '—'}</dd></div>
						<div class="flex justify-between"><dt class="text-zinc-500">Branch</dt><dd>{app.gitBranch || app.autoDeployBranch || '—'}</dd></div>
						<div class="flex justify-between"><dt class="text-zinc-500">Last commit</dt><dd class="font-mono text-xs">{app.lastDeployedCommitSha || '—'}</dd></div>
					</dl>
				</Card>

				{#if watchedID !== null}
					<Card title="Live deploy">
						<dl class="space-y-2 text-sm">
							<div class="flex justify-between">
								<dt class="text-zinc-500">Deployment</dt>
								<dd>#{watchedID}</dd>
							</div>
							<div class="flex justify-between">
								<dt class="text-zinc-500">Status</dt>
								<dd>{watchedStatus}</dd>
							</div>
							<div class="flex justify-between">
								<dt class="text-zinc-500">Phase</dt>
								<dd>{watchedPhase || '—'}</dd>
							</div>
						</dl>
						<div class="mt-3">
							{#key watchedID}
								<LogStream topic={`deploy.${watchedID}`} height="14rem" showStream={false} />
							{/key}
						</div>
					</Card>
				{:else}
					<Card title="Live metrics (30 min)">
						{#if metrics.length === 0}
							<p class="text-sm text-zinc-500">
								No samples yet. The scraper polls every 15s; data appears after the first deploy.
							</p>
						{:else}
							<dl class="space-y-3 text-sm">
								<div>
									<div class="flex items-baseline justify-between">
										<dt class="text-zinc-500">CPU %</dt>
										<dd class="font-mono text-xs">{cpuSeries[cpuSeries.length - 1]?.toFixed(1) ?? '—'}</dd>
									</div>
									<Sparkline points={cpuSeries} width={300} height={40} />
								</div>
								<div>
									<div class="flex items-baseline justify-between">
										<dt class="text-zinc-500">Memory (MiB)</dt>
										<dd class="font-mono text-xs">{memSeries[memSeries.length - 1]?.toFixed(0) ?? '—'}</dd>
									</div>
									<Sparkline points={memSeries} width={300} height={40} stroke="#8b5cf6" fill="rgba(139,92,246,0.10)" />
								</div>
							</dl>
						{/if}
					</Card>
				{/if}
			</div>
		{:else if tab === 'logs'}
			{#key tab}
				<LogsPanel {slug} />
			{/key}
		{:else if tab === 'env'}
			{#key tab}
				<EnvVarsPanel {slug} />
			{/key}
		{:else if tab === 'volumes'}
			{#key tab}
				<VolumesPanel appSlug={slug} />
			{/key}
		{:else if tab === 'deployments'}
			<Card>
				{#if deployments.length === 0}
					<div class="text-sm text-zinc-500">No deployments yet.</div>
				{:else}
					<table class="w-full text-sm">
						<thead class="text-left text-xs uppercase text-zinc-500">
							<tr>
								<th class="pb-2">#</th>
								<th class="pb-2">Color</th>
								<th class="pb-2">Status</th>
								<th class="pb-2">Trigger</th>
								<th class="pb-2">Commit</th>
								<th class="pb-2">Started</th>
								<th class="pb-2">Failure</th>
								<th class="pb-2"></th>
							</tr>
						</thead>
						<tbody>
							{#each deployments as d}
								<tr class="border-t border-zinc-100">
									<td class="py-2 font-mono">{d.id}</td>
									<td class="py-2">{d.color}</td>
									<td class="py-2">{d.status}</td>
									<td class="py-2 text-zinc-600">—</td>
									<td class="py-2 font-mono text-xs">{d.commitSha ? d.commitSha.slice(0, 7) : '—'}</td>
									<td class="py-2 text-zinc-500">
										{d.startedAt ? new Date(d.startedAt).toLocaleString() : '—'}
									</td>
									<td class="py-2 text-red-600">{d.failureReason || ''}</td>
									<td class="py-2 text-right">
										<button class="text-sm text-teal-700 hover:underline" onclick={() => openDeployLog(d.id)}>
											View log
										</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{/if}
			</Card>
		{:else}
			<!-- Settings tab -->
			{#if revealedSecret}
				<Card title="Copy the webhook secret now — it will not be shown again">
					<div class="flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-900 px-3 py-2 text-sm text-teal-300"
							>{revealedSecret}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedSecret!)}>Copy</Button>
						<Button variant="secondary" onclick={() => (revealedSecret = null)}>Dismiss</Button>
					</div>
				</Card>
			{/if}
			{#if revealedPublicKey}
				<Card title="Copy the SSH public key into GitHub now — paste it as a Deploy key">
					<div class="flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-50 px-3 py-2 font-mono text-xs text-zinc-800"
							>{revealedPublicKey}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedPublicKey!)}>Copy</Button>
					</div>
					{#if revealedFingerprint}
						<p class="mt-2 font-mono text-xs text-zinc-500">Fingerprint: {revealedFingerprint}</p>
					{/if}
					<div class="mt-3">
						<Button variant="secondary" onclick={() => (revealedPublicKey = null)}>Dismiss</Button>
					</div>
				</Card>
			{/if}

			<Card title="Git source">
				<form
					onsubmit={(e) => {
						e.preventDefault();
						void saveSettings();
					}}
					class="space-y-4"
				>
					<div>
						<label for="giturl" class="mb-1 block text-sm font-medium text-zinc-700">
							Git URL (https or ssh). Leave empty to paste compose manually.
						</label>
						<Input id="giturl" bind:value={formGitUrl} placeholder="https://github.com/owner/repo.git" />
					</div>
					<div class="grid grid-cols-2 gap-4">
						<div>
							<label for="gitbranch" class="mb-1 block text-sm font-medium text-zinc-700">
								Branch
							</label>
							<Input id="gitbranch" bind:value={formGitBranch} placeholder="main" />
						</div>
						<div>
							<label for="gitpath" class="mb-1 block text-sm font-medium text-zinc-700">
								Compose path inside the repo
							</label>
							<Input id="gitpath" bind:value={formGitComposePath} placeholder="docker-compose.yml" />
						</div>
					</div>
					<div>
						<label for="authkind" class="mb-1 block text-sm font-medium text-zinc-700">
							Authentication
						</label>
						<select
							id="authkind"
							bind:value={formGitAuthKind}
							class="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm"
						>
							<option value="">Public (no auth)</option>
							<option value="ssh">SSH deploy key (Teal generates)</option>
							<option value="pat">Personal access token</option>
							<option value="github_app">GitHub App (recommended)</option>
						</select>
					</div>
					{#if formGitAuthKind === 'github_app'}
						<div class="rounded-md border border-zinc-200 bg-zinc-50 p-3 text-sm">
							{#if app.githubAppInstallationId}
								<div class="flex items-center justify-between gap-3">
									<div>
										<div class="font-medium text-zinc-800">
											Installed
											{#if app.githubAppRepo}
												on <code class="font-mono">{app.githubAppRepo}</code>
											{/if}
										</div>
										<div class="text-xs text-zinc-500">
											Installation ID: {app.githubAppInstallationId}
										</div>
									</div>
									<Button
										variant="secondary"
										onclick={async () => {
											try {
												const r = await appsApi.startGitHubAppInstall(slug);
												location.href = r.installUrl;
											} catch (err) {
												alert(err instanceof ApiError ? err.message : 'Could not start install');
											}
										}}
									>
										Reconfigure
									</Button>
								</div>
							{:else}
								<div class="flex items-center justify-between gap-3">
									<p class="text-zinc-700">
										After saving, click below to install the platform GitHub App on this repo.
										Requires the platform App to be configured at <code>/settings/github-app</code>.
									</p>
									<Button
										onclick={async () => {
											try {
												const r = await appsApi.startGitHubAppInstall(slug);
												location.href = r.installUrl;
											} catch (err) {
												alert(err instanceof ApiError ? err.message : 'Could not start install');
											}
										}}
									>
										Install on a repo
									</Button>
								</div>
								<div class="mt-3 border-t border-zinc-200 pt-3">
									<label for="manualInstall" class="mb-1 block text-xs font-medium text-zinc-600">
										Manual fallback — paste the installation ID
									</label>
									<div class="flex gap-2">
										<Input
											id="manualInstall"
											bind:value={manualInstallationID}
											placeholder="e.g. 12345678"
										/>
										<Button
											variant="secondary"
											disabled={!manualInstallationID}
											onclick={async () => {
												const idNum = Number(manualInstallationID);
												if (!Number.isInteger(idNum) || idNum <= 0) {
													alert('Installation ID must be a positive integer');
													return;
												}
												try {
													await appsApi.update(slug, { githubAppInstallationId: idNum });
													await loadApp();
												} catch (err) {
													alert(err instanceof ApiError ? err.message : 'Save failed');
												}
											}}
										>
											Link
										</Button>
									</div>
									<p class="mt-1 text-xs text-zinc-500">
										Use this when the install-then-redirect flow doesn't work (e.g. the App's
										<strong>Setup URL</strong> wasn't configured). Find the ID at
										<a class="text-teal-700 underline" target="_blank" rel="noopener" href="https://github.com/settings/installations">
											github.com/settings/installations
										</a>
										— click your installed app, the URL ends with the ID.
									</p>
								</div>
							{/if}
						</div>
					{/if}
					{#if formGitAuthKind === 'pat'}
						<div>
							<label for="credential" class="mb-1 block text-sm font-medium text-zinc-700">
								Personal access token
							</label>
							<Input
								id="credential"
								type="password"
								bind:value={formGitCredential}
								placeholder={app.hasGitCredential ? '(leave empty to keep existing)' : 'ghp_…'}
							/>
						</div>
					{/if}
					<label class="flex items-center gap-2 text-sm text-zinc-700">
						<input type="checkbox" bind:checked={formAutoDeploy} />
						Auto-deploy on webhook push to this branch
					</label>
					{#if settingsError}
						<div class="text-sm text-red-600">{settingsError}</div>
					{/if}
					<div class="flex justify-end">
						<Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
					</div>
				</form>
			</Card>

			{#if app.gitAuthKind === 'ssh' && app.hasGitCredential && existingPublicKey}
				<Card title="SSH deploy key">
					<p class="mb-2 text-sm text-zinc-600">
						Paste this public key into GitHub → repo → Settings → Deploy keys.
					</p>
					<div class="flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-50 px-3 py-2 font-mono text-xs text-zinc-800"
							>{existingPublicKey}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(existingPublicKey!)}>Copy</Button>
					</div>
					{#if existingFingerprint}
						<p class="mt-2 font-mono text-xs text-zinc-500">Fingerprint: {existingFingerprint}</p>
					{/if}
					<div class="mt-3 flex justify-end">
						<Button variant="secondary" onclick={rotateDeployKey}>Rotate deploy key</Button>
					</div>
				</Card>
			{/if}

			{#if app.gitUrl && app.hasWebhookSecret}
				<Card title="Webhook">
					<p class="mb-2 text-sm text-zinc-600">
						Configure this URL in GitHub → repo → Settings → Webhooks. Content type
						<code>application/json</code>. Secret is the value shown on initial save / rotate.
					</p>
					<div class="flex items-center gap-2">
						<code class="flex-1 break-all rounded bg-zinc-50 px-3 py-2 font-mono text-xs text-zinc-800"
							>{webhookURL}</code
						>
						<Button variant="secondary" onclick={() => copyToClipboard(webhookURL)}>Copy</Button>
					</div>
					<div class="mt-3 flex justify-end">
						<Button variant="secondary" onclick={rotateWebhookSecret}>Rotate webhook secret</Button>
					</div>
				</Card>
			{/if}

			<Card title="Resource limits">
				<p class="mb-3 text-sm text-zinc-500">
					Applied to every service in this app's compose. Empty disables the limit.
				</p>
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label for="cpu" class="mb-1 block text-sm font-medium text-zinc-700">CPU</label>
						<Input id="cpu" bind:value={formCPULimit} placeholder="0.5" />
						<p class="mt-1 text-xs text-zinc-500">Number of CPUs (e.g. 0.5, 2).</p>
					</div>
					<div>
						<label for="mem" class="mb-1 block text-sm font-medium text-zinc-700">Memory</label>
						<Input id="mem" bind:value={formMemoryLimit} placeholder="512m" />
						<p class="mt-1 text-xs text-zinc-500">Compose grammar (256m, 1g).</p>
					</div>
				</div>
			</Card>

			<Card title="Notifications">
				<p class="mb-3 text-sm text-zinc-500">
					On every terminal deploy, Teal can POST a signed JSON event and email on failure.
				</p>
				<div class="space-y-3">
					<div>
						<label for="hook" class="mb-1 block text-sm font-medium text-zinc-700">
							Outbound webhook URL
						</label>
						<Input id="hook" bind:value={formNotifyWebhookUrl} placeholder="https://hooks.example.com/teal" />
					</div>
					{#if app.hasNotificationSecret}
						<div class="flex justify-end">
							<Button
								variant="secondary"
								onclick={async () => {
									if (!confirm('Rotate the outbound webhook secret? Update the receiver afterwards.')) return;
									try {
										const r = await appsApi.rotateNotificationSecret(slug);
										revealedSecret = r.webhookSecret;
									} catch (err) {
										alert(err instanceof ApiError ? err.message : 'Rotate failed');
									}
								}}
							>
								Rotate notification secret
							</Button>
						</div>
					{/if}
					<div>
						<label for="nemail" class="mb-1 block text-sm font-medium text-zinc-700">
							Failure email recipient
						</label>
						<Input id="nemail" type="email" bind:value={formNotifyEmail} placeholder="ops@example.com" />
						<p class="mt-1 text-xs text-zinc-500">
							Sent on deploy failure only. SMTP must be configured in Platform settings.
						</p>
					</div>
				</div>
			</Card>
		{/if}
	{/if}
</div>

{#if logModalDepID !== null}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-zinc-900/50 p-4"
		role="dialog"
		aria-modal="true"
	>
		<div class="flex max-h-[80vh] w-full max-w-4xl flex-col rounded-lg bg-white shadow-xl">
			<div class="flex items-center justify-between border-b border-zinc-200 px-4 py-3">
				<h2 class="text-lg font-medium text-zinc-900">Deployment #{logModalDepID} log</h2>
				<button
					class="text-sm text-zinc-500 hover:text-zinc-800"
					onclick={() => {
						logModalDepID = null;
						logModalText = '';
					}}
				>
					Close
				</button>
			</div>
			<div class="flex-1 overflow-auto bg-zinc-950 p-3 font-mono text-xs leading-snug text-zinc-100">
				{#if logModalLoading}
					<div class="text-zinc-400">Loading…</div>
				{:else}
					<pre class="whitespace-pre-wrap break-all">{logModalText || '(empty)'}</pre>
				{/if}
			</div>
		</div>
	</div>
{/if}
