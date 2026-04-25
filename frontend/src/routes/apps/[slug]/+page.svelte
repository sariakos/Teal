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
		GitAuthKind,
		Route
	} from '$lib/api/types';
	import { servicesApi, type ServiceInfo } from '$lib/api/services';
	import { githubAppReposApi, type AppReposResponse } from '$lib/api/github_app';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Input from '$lib/components/Input.svelte';
	import EnvVarsPanel from '$lib/components/EnvVarsPanel.svelte';
	import VolumesPanel from '$lib/components/VolumesPanel.svelte';
	import LogsPanel from '$lib/components/LogsPanel.svelte';
	import LogStream from '$lib/components/LogStream.svelte';
	import Sparkline from '$lib/components/Sparkline.svelte';
	import { metricsApi, logsApi, type MetricSample } from '$lib/api/logs';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { dirty } from '$lib/stores/dirty.svelte';

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
	let watchStartedAt = 0;

	// Wall-clock cap on how long we keep polling. The backend should mark
	// every deployment terminal eventually, but bugs and crashes can leave
	// it hanging in 'running' — without this guard the UI gets stuck on
	// "Deploying…" until the user reloads. 15 min covers even slow image
	// builds; longer than that and the user should investigate manually.
	const WATCH_TIMEOUT_MS = 15 * 60 * 1000;

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
		stopWatching();
		watchedID = id;
		watchedPhase = 'pending';
		watchedStatus = 'pending';
		watchStartedAt = Date.now();
		pollHandle = setInterval(async () => {
			if (Date.now() - watchStartedAt > WATCH_TIMEOUT_MS) {
				stopWatching();
				toast.warning('Stopped watching deploy', {
					description:
						'Deployment ran longer than expected. Check the Deployments tab for current status.'
				});
				await Promise.all([loadApp(), loadDeployments()]);
				return;
			}
			try {
				const dep = await deploymentsApi.get(id);
				watchedPhase = dep.phase ?? '';
				watchedStatus = dep.status;
				if (dep.status !== 'pending' && dep.status !== 'running') {
					stopWatching();
					await Promise.all([loadApp(), loadDeployments()]);
					if (dep.status === 'succeeded') {
						dirty.clear(slug);
						toast.success(`Deploy #${id} succeeded`);
					} else if (dep.status === 'failed') {
						toast.error(`Deploy #${id} failed`, {
							description: dep.failureReason || 'See deployment log for details.'
						});
					} else if (dep.status === 'canceled') {
						toast.info(`Deploy #${id} canceled`);
					}
				}
			} catch (err) {
				stopWatching();
				toast.error('Lost connection while watching deploy', {
					description: err instanceof Error ? err.message : undefined
				});
			}
		}, 1000);
	}

	function stopWatching() {
		if (pollHandle) {
			clearInterval(pollHandle);
			pollHandle = null;
		}
		watchedID = null;
		watchedPhase = '';
		watchedStatus = '';
	}

	async function handleDeploy() {
		try {
			const dep = await appsApi.deploy(slug);
			toast.info(`Deploy #${dep.id} started`);
			await Promise.all([loadApp(), loadDeployments()]);
			startWatching(dep.id);
		} catch (err) {
			toast.error('Deploy failed to start', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	async function handleRollback() {
		if (
			!(await dialog.confirm({
				title: 'Roll back to the previous deploy?',
				body: 'Traffic will switch back to the previously running color. The current color stays around for a one-click roll-forward.',
				confirmLabel: 'Roll back'
			}))
		)
			return;
		try {
			const dep = await appsApi.rollback(slug);
			toast.info(`Rollback #${dep.id} started`);
			await Promise.all([loadApp(), loadDeployments()]);
			startWatching(dep.id);
		} catch (err) {
			toast.error('Rollback failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	async function handleDelete() {
		if (!app) return;
		const typed = await dialog.prompt({
			title: 'Delete this app?',
			body: 'Containers stop and the app config is removed. Volumes are kept — clean them up in the Volumes tab if you want them gone.',
			tone: 'danger',
			expect: app.slug,
			placeholder: app.slug,
			help: 'Type the slug to confirm.',
			confirmLabel: 'Delete app'
		});
		if (typed !== app.slug) return;
		try {
			await appsApi.delete(slug);
			toast.success(`App "${app.slug}" deleted`);
			goto('/');
		} catch (err) {
			toast.error('Delete failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	function cancelWatching() {
		stopWatching();
		toast.info('Stopped watching — deploy continues in background.');
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

	// GitHub App repos: populated when the user picks github_app auth.
	// Keyed by installation so we can render an optgroup per owner.
	let ghaRepos = $state<AppReposResponse | null>(null);
	let ghaReposLoading = $state(false);
	let ghaReposError = $state<string | null>(null);
	// Selection encoded as "<installationId>::<full_name>" so a single
	// <select> can carry both the installation ID and the repo path.
	let selectedRepoKey = $state<string>('');
	let linkingRepo = $state(false);
	let settingsError = $state<string | null>(null);
	let saving = $state(false);

	// ------- Routes card state -------
	// services: compose-parsed list from GET /apps/{slug}/services. Keyed
	// by service name in routeByService so the per-service inputs survive
	// re-fetches without clobbering unsaved edits.
	let services = $state<ServiceInfo[]>([]);
	let servicesSource = $state<'checkout' | 'stored' | 'none' | ''>('');
	let servicesHint = $state<string>('');
	let servicesError = $state<string | null>(null);
	let servicesLoading = $state(false);
	// Per-service domain + optional port. "" domain means no route for
	// this service (skipped on save). Port "" means auto-probe.
	let routeByService = $state<Record<string, { domain: string; port: string }>>({});
	let routesSaving = $state(false);
	let routesError = $state<string | null>(null);
	let routesSaved = $state(false);

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

	function seedRoutesFromApp(a: AppDetail, svcs: ServiceInfo[]) {
		// Build a fresh map keyed by every known service plus any
		// "phantom" service names that appear in saved Routes but not
		// in the current compose (so the user can still edit/delete
		// stale routes after a service rename).
		const next: Record<string, { domain: string; port: string }> = {};
		for (const s of svcs) {
			next[s.name] = { domain: '', port: '' };
		}
		for (const r of a.routes ?? []) {
			const key = r.service ?? '';
			if (!(key in next)) next[key] = { domain: '', port: '' };
			next[key] = {
				domain: r.domain ?? '',
				port: r.port ? String(r.port) : ''
			};
		}
		routeByService = next;
	}

	async function loadServices() {
		servicesLoading = true;
		servicesError = null;
		try {
			const r = await servicesApi.list(slug);
			services = r.services;
			servicesSource = r.source;
			servicesHint = r.hint ?? '';
			if (app) seedRoutesFromApp(app, services);
		} catch (err) {
			servicesError = err instanceof ApiError ? err.message : 'Could not load services';
			services = [];
			servicesSource = '';
		} finally {
			servicesLoading = false;
		}
	}

	async function saveRoutes() {
		if (!app) return;
		routesSaving = true;
		routesError = null;
		routesSaved = false;
		try {
			const routes: Route[] = [];
			for (const [svc, v] of Object.entries(routeByService)) {
				const domain = v.domain.trim();
				if (!domain) continue;
				const r: Route = { domain };
				if (svc !== '') r.service = svc;
				if (v.port.trim() !== '') {
					const n = Number(v.port);
					if (!Number.isInteger(n) || n <= 0 || n > 65535) {
						routesError = `Port for "${svc}" must be 1–65535`;
						routesSaving = false;
						return;
					}
					r.port = n;
				}
				routes.push(r);
			}
			const resp = await appsApi.update(slug, { routes });
			app = resp;
			seedRoutesFromApp(resp, services);
			routesSaved = true;
			dirty.mark(slug);
			toast.success('Routes saved', { description: 'Redeploy to apply.' });
		} catch (err) {
			routesError = err instanceof ApiError ? err.message : 'Save failed';
			toast.error('Saving routes failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			routesSaving = false;
		}
	}

	async function loadGitHubAppRepos() {
		ghaReposLoading = true;
		ghaReposError = null;
		try {
			ghaRepos = await githubAppReposApi.list(slug);
			// Pre-select the currently linked repo, if any.
			if (app?.githubAppInstallationId && app.githubAppRepo) {
				selectedRepoKey = `${app.githubAppInstallationId}::${app.githubAppRepo}`;
			}
		} catch (err) {
			ghaReposError = err instanceof ApiError ? err.message : 'Could not load repos';
			ghaRepos = null;
		} finally {
			ghaReposLoading = false;
		}
	}

	async function linkSelectedRepo() {
		if (!selectedRepoKey) return;
		const sep = selectedRepoKey.indexOf('::');
		if (sep < 0) return;
		const idNum = Number(selectedRepoKey.slice(0, sep));
		const fullName = selectedRepoKey.slice(sep + 2);
		if (!Number.isInteger(idNum) || idNum <= 0 || !fullName) return;
		linkingRepo = true;
		try {
			const resp = await appsApi.update(slug, {
				githubAppInstallationId: idNum,
				githubAppRepo: fullName,
				gitUrl: `https://github.com/${fullName}.git`,
				gitAuthKind: 'github_app'
			});
			app = resp;
			await loadGitHubAppRepos();
			dirty.mark(slug);
			toast.success(`Linked to ${fullName}`);
		} catch (err) {
			toast.error('Link failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			linkingRepo = false;
		}
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
			void loadServices();
			if (formGitAuthKind === 'github_app' || app.gitAuthKind === 'github_app') {
				void loadGitHubAppRepos();
			}
		}
		if (tab === 'overview' && app) {
			void loadMetrics();
		}
	});

	// Re-fetch repos when the user flips the auth dropdown to github_app.
	$effect(() => {
		if (tab === 'settings' && formGitAuthKind === 'github_app' && !ghaRepos && !ghaReposLoading) {
			void loadGitHubAppRepos();
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
			dirty.mark(slug);
			toast.success('Settings saved', { description: 'Redeploy to apply.' });
		} catch (err) {
			settingsError = err instanceof ApiError ? err.message : 'Save failed';
			toast.error('Saving settings failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			saving = false;
		}
	}

	async function rotateDeployKey() {
		if (
			!(await dialog.confirm({
				title: 'Rotate the SSH deploy key?',
				body: 'You must paste the new public key into GitHub before the next deploy, or it will fail.',
				tone: 'warning',
				confirmLabel: 'Rotate'
			}))
		)
			return;
		try {
			const k = await appsApi.rotateDeployKey(slug);
			revealedPublicKey = k.publicKey;
			revealedFingerprint = k.fingerprint;
			await loadApp();
			await loadExistingDeployKey();
			toast.success('Deploy key rotated', { description: 'Update GitHub with the new public key.' });
		} catch (err) {
			toast.error('Rotate failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	async function rotateWebhookSecret() {
		if (
			!(await dialog.confirm({
				title: 'Rotate the webhook secret?',
				body: 'Update GitHub with the new value afterwards or pushes will be rejected.',
				tone: 'warning',
				confirmLabel: 'Rotate'
			}))
		)
			return;
		try {
			const { webhookSecret } = await appsApi.rotateWebhookSecret(slug);
			revealedSecret = webhookSecret;
			await loadApp();
			toast.success('Webhook secret rotated');
		} catch (err) {
			toast.error('Rotate failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
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
				<div class="flex items-center gap-2">
					<h1 class="text-2xl font-semibold text-zinc-900">{app.name}</h1>
					{#if dirty.has(slug)}
						<span
							class="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-warning-soft)] px-2 py-0.5 text-xs font-medium text-[var(--color-warning-soft-fg)]"
							title="Configuration changed — redeploy to apply"
						>
							<span class="h-1.5 w-1.5 rounded-full bg-[var(--color-warning)]"></span>
							Pending changes
						</span>
					{/if}
				</div>
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
						<div class="flex justify-between gap-2">
							<dt class="text-zinc-500">URLs</dt>
							<dd class="text-right">
								{#if (app.routes ?? []).length === 0}
									<span class="text-zinc-400">none — add a route in Settings</span>
								{:else}
									<ul class="space-y-0.5">
										{#each app.routes as r}
											<li>
												<a
													class="text-teal-700 hover:underline"
													href={`https://${r.domain}`}
													target="_blank"
													rel="noopener"
												>
													{r.domain}
												</a>
												{#if r.service}
													<span class="ml-1 text-xs text-zinc-500">→ {r.service}</span>
												{/if}
											</li>
										{/each}
									</ul>
								{/if}
							</dd>
						</div>
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
						<div class="mt-3 flex justify-end">
							<button
								type="button"
								onclick={cancelWatching}
								class="text-xs text-zinc-500 hover:text-zinc-700 hover:underline"
							>
								Stop watching
							</button>
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

			<Card title="Routes">
				<p class="mb-3 text-sm text-zinc-500">
					Give each service its own public hostname. Leave a service blank to keep it
					private. Ports are auto-detected — only override when the probe gets it wrong.
				</p>
				{#if servicesLoading}
					<p class="text-sm text-zinc-500">Loading services…</p>
				{:else if servicesError}
					<p class="text-sm text-red-600">{servicesError}</p>
				{:else if servicesSource === 'none'}
					<p class="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-600">
						{servicesHint || 'No compose available yet — deploy at least once so Teal can list services.'}
					</p>
				{:else if services.length === 0}
					<p class="text-sm text-zinc-500">No services declared in the compose file.</p>
				{:else}
					<form
						onsubmit={(e) => {
							e.preventDefault();
							void saveRoutes();
						}}
						class="space-y-3"
					>
						<div class="overflow-hidden rounded-md border border-zinc-200">
							<table class="w-full text-sm">
								<thead class="bg-zinc-50 text-xs uppercase text-zinc-500">
									<tr>
										<th class="px-3 py-2 text-left">Service</th>
										<th class="px-3 py-2 text-left">Domain</th>
										<th class="px-3 py-2 text-left">Port</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-zinc-100">
									{#each services as svc (svc.name)}
										<tr>
											<td class="px-3 py-2 align-top">
												<div class="font-medium text-zinc-800">{svc.name}</div>
												<div class="text-xs text-zinc-500">
													{svc.image || (svc.hasBuild ? 'built from source' : '—')}
													{#if svc.exposedPorts && svc.exposedPorts.length > 0}
														· ports: {svc.exposedPorts.join(', ')}
													{/if}
												</div>
											</td>
											<td class="px-3 py-2 align-top">
												<Input
													bind:value={routeByService[svc.name].domain}
													placeholder="api.example.com"
												/>
											</td>
											<td class="px-3 py-2 align-top">
												<Input
													bind:value={routeByService[svc.name].port}
													placeholder="auto"
												/>
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
						{#if routesError}
							<div class="text-sm text-red-600">{routesError}</div>
						{/if}
						{#if routesSaved}
							<div class="text-sm text-teal-700">Saved — redeploy to apply.</div>
						{/if}
						<div class="flex items-center justify-between">
							<p class="text-xs text-zinc-500">
								Source: {servicesSource}. HTTPS is added automatically once an LE cert is issued.
							</p>
							<Button type="submit" disabled={routesSaving}>
								{routesSaving ? 'Saving…' : 'Save routes'}
							</Button>
						</div>
					</form>
				{/if}
			</Card>

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
						<div class="space-y-3 rounded-md border border-zinc-200 bg-zinc-50 p-3 text-sm">
							{#if ghaReposLoading}
								<p class="text-zinc-500">Loading installations…</p>
							{:else if ghaReposError}
								<p class="text-red-600">{ghaReposError}</p>
							{:else if !ghaRepos || !ghaRepos.configured}
								<p class="text-zinc-700">
									The platform GitHub App isn't configured yet. Set it up at
									<a class="text-teal-700 underline" href="/settings/github-app">
										Settings → GitHub App
									</a>
									first (one-click flow available there), then come back here.
								</p>
							{:else if ghaRepos.installations.length === 0}
								<div class="space-y-2">
									<p class="text-zinc-700">
										The platform App isn't installed on any repos yet. Install it on at least
										one repo, then refresh.
									</p>
									{#if ghaRepos.appSlug}
										<a
											class="inline-flex items-center rounded-md bg-teal-600 px-3 py-2 text-xs font-medium text-white hover:bg-teal-700"
											target="_blank"
											rel="noopener"
											href={`https://github.com/apps/${ghaRepos.appSlug}/installations/new`}
										>
											Install on GitHub →
										</a>
									{/if}
								</div>
							{:else}
								<label for="repoPick" class="block text-xs font-medium text-zinc-600">
									Pick a repo
								</label>
								<select
									id="repoPick"
									bind:value={selectedRepoKey}
									class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm"
								>
									<option value="">Select a repository…</option>
									{#each ghaRepos.installations as inst (inst.installationId)}
										<optgroup label={inst.accountLogin}>
											{#if inst.repos.length === 0}
												<option disabled>(no repos accessible — adjust on GitHub)</option>
											{:else}
												{#each inst.repos as r (r.fullName)}
													<option value={`${inst.installationId}::${r.fullName}`}>
														{r.fullName}{r.private ? ' 🔒' : ''}
													</option>
												{/each}
											{/if}
										</optgroup>
									{/each}
								</select>
								<div class="flex items-center justify-between gap-2">
									<p class="text-xs text-zinc-500">
										Saving links this app to the picked repo + installation. The Git URL +
										auth fields are filled in for you.
									</p>
									<Button
										variant="secondary"
										disabled={!selectedRepoKey || linkingRepo}
										onclick={() => void linkSelectedRepo()}
									>
										{linkingRepo ? 'Linking…' : 'Link repo'}
									</Button>
								</div>
								{#if ghaRepos.appSlug}
									<p class="text-xs text-zinc-500">
										Don't see the repo you want?
										<a
											class="text-teal-700 underline"
											target="_blank"
											rel="noopener"
											href={`https://github.com/apps/${ghaRepos.appSlug}/installations/new`}
										>
											Install on more repos →
										</a>
									</p>
								{/if}
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
									if (
										!(await dialog.confirm({
											title: 'Rotate notification secret?',
											body: 'Update the receiver afterwards or it will reject signed events.',
											tone: 'warning',
											confirmLabel: 'Rotate'
										}))
									)
										return;
									try {
										const r = await appsApi.rotateNotificationSecret(slug);
										revealedSecret = r.webhookSecret;
										toast.success('Notification secret rotated');
									} catch (err) {
										toast.error('Rotate failed', {
											description: err instanceof ApiError ? err.message : undefined
										});
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
