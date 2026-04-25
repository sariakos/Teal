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
	import Select from '$lib/components/Select.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import StatusDot from '$lib/components/StatusDot.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import EnvVarsPanel from '$lib/components/EnvVarsPanel.svelte';
	import VolumesPanel from '$lib/components/VolumesPanel.svelte';
	import LogsPanel from '$lib/components/LogsPanel.svelte';
	import LogStream from '$lib/components/LogStream.svelte';
	import Sparkline from '$lib/components/Sparkline.svelte';
	import { metricsApi, logsApi, type MetricSample } from '$lib/api/logs';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { dirty } from '$lib/stores/dirty.svelte';
	import { Play, Undo2, Trash2, Copy, X, Rocket } from '@lucide/svelte';

	const slug = $derived(page.params.slug as string);

	type Tab = 'overview' | 'deployments' | 'logs' | 'env' | 'volumes' | 'settings';
	let tab = $state<Tab>('overview');

	const tabs: { value: Tab; label: string }[] = [
		{ value: 'overview', label: 'Overview' },
		{ value: 'deployments', label: 'Deployments' },
		{ value: 'logs', label: 'Logs' },
		{ value: 'env', label: 'Env' },
		{ value: 'volumes', label: 'Volumes' },
		{ value: 'settings', label: 'Settings' }
	];

	type StatusTone = 'neutral' | 'warning' | 'success' | 'danger' | 'info' | 'accent';
	const appStatusTone: Record<string, StatusTone> = {
		idle: 'neutral',
		deploying: 'warning',
		running: 'success',
		failed: 'danger',
		stopped: 'neutral'
	};
	const depStatusTone: Record<string, StatusTone> = {
		pending: 'warning',
		running: 'warning',
		succeeded: 'success',
		failed: 'danger',
		canceled: 'neutral'
	};

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
		<div class="text-sm text-[var(--color-danger)]">{appError}</div>
	{:else if !app}
		<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
	{:else}
		<PageHeader title={app.name} eyebrow={app.slug}>
			{#snippet extra()}
				<Badge tone={appStatusTone[app!.status] ?? 'neutral'}>
					<StatusDot
						tone={appStatusTone[app!.status] ?? 'neutral'}
						pulse={app!.status === 'deploying'}
					/>
					{app!.status}
				</Badge>
				{#if dirty.has(slug)}
					<Badge tone="warning" class="cursor-default" >
						<span class="h-1.5 w-1.5 rounded-full bg-[var(--color-warning)]"></span>
						Pending changes
					</Badge>
				{/if}
			{/snippet}
			{#snippet actions()}
				<Button onclick={handleDeploy} disabled={watchedID !== null}>
					{#if watchedID !== null}
						<StatusDot tone="warning" pulse class="mr-1" />
						Deploying…
					{:else}
						<Play class="h-4 w-4" />
						Deploy
					{/if}
				</Button>
				<Button variant="secondary" onclick={handleRollback} disabled={watchedID !== null}>
					<Undo2 class="h-4 w-4" />
					Rollback
				</Button>
				<Button variant="ghost" onclick={handleDelete} title="Delete app">
					<Trash2 class="h-4 w-4 text-[var(--color-danger)]" />
				</Button>
			{/snippet}
		</PageHeader>

		<Tabs {tabs} bind:value={tab} />

		{#if tab === 'overview'}
			{#if app.status === 'failed'}
				<div
					class="rounded-lg border border-[var(--color-danger-soft)] bg-[var(--color-danger-soft)] px-4 py-3 text-sm text-[var(--color-danger-soft-fg)]"
				>
					<div class="flex items-center justify-between gap-3">
						<div>
							<div class="font-semibold">Last deploy failed</div>
							<div class="mt-0.5 text-xs">
								Check the Deployments tab for the captured log; fix and retry.
							</div>
						</div>
						<Button onclick={handleDeploy} disabled={watchedID !== null}>
							<Rocket class="h-4 w-4" />
							Retry deploy
						</Button>
					</div>
				</div>
			{/if}
			<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
				<Card title="Status">
					<dl class="space-y-2.5 text-sm">
						<div class="flex justify-between gap-2">
							<dt class="text-[var(--color-fg-muted)]">Active color</dt>
							<dd class="font-medium text-[var(--color-fg)]">{app.activeColor || '—'}</dd>
						</div>
						<div class="flex justify-between gap-2">
							<dt class="text-[var(--color-fg-muted)]">URLs</dt>
							<dd class="text-right">
								{#if (app.routes ?? []).length === 0}
									<span class="text-[var(--color-fg-subtle)]">
										none — add a route in Settings
									</span>
								{:else}
									<ul class="space-y-0.5">
										{#each app.routes as r}
											<li>
												<a
													class="text-[var(--color-accent)] hover:underline"
													href={`https://${r.domain}`}
													target="_blank"
													rel="noopener"
												>
													{r.domain}
												</a>
												{#if r.service}
													<span class="ml-1 text-xs text-[var(--color-fg-subtle)]">
														→ {r.service}
													</span>
												{/if}
											</li>
										{/each}
									</ul>
								{/if}
							</dd>
						</div>
						<div class="flex justify-between gap-2">
							<dt class="text-[var(--color-fg-muted)]">Branch</dt>
							<dd class="font-mono text-xs text-[var(--color-fg)]">
								{app.gitBranch || app.autoDeployBranch || '—'}
							</dd>
						</div>
						<div class="flex justify-between gap-2">
							<dt class="text-[var(--color-fg-muted)]">Last commit</dt>
							<dd class="font-mono text-xs text-[var(--color-fg)]">
								{app.lastDeployedCommitSha || '—'}
							</dd>
						</div>
					</dl>
				</Card>

				{#if watchedID !== null}
					<Card title="Live deploy">
						{#snippet actions()}
							<button
								type="button"
								onclick={cancelWatching}
								class="inline-flex items-center gap-1 text-xs text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
							>
								<X class="h-3.5 w-3.5" />
								Stop watching
							</button>
						{/snippet}
						<dl class="space-y-2 text-sm">
							<div class="flex justify-between">
								<dt class="text-[var(--color-fg-muted)]">Deployment</dt>
								<dd class="font-mono">#{watchedID}</dd>
							</div>
							<div class="flex items-center justify-between">
								<dt class="text-[var(--color-fg-muted)]">Status</dt>
								<dd>
									<Badge tone={depStatusTone[watchedStatus] ?? 'neutral'} size="sm">
										<StatusDot
											tone={depStatusTone[watchedStatus] ?? 'neutral'}
											pulse={watchedStatus === 'pending' || watchedStatus === 'running'}
										/>
										{watchedStatus}
									</Badge>
								</dd>
							</div>
							<div class="flex justify-between">
								<dt class="text-[var(--color-fg-muted)]">Phase</dt>
								<dd class="font-mono text-xs">{watchedPhase || '—'}</dd>
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
							<p class="text-sm text-[var(--color-fg-muted)]">
								No samples yet. The scraper polls every 15s; data appears after the first deploy.
							</p>
						{:else}
							<dl class="space-y-3 text-sm">
								<div>
									<div class="flex items-baseline justify-between">
										<dt class="text-[var(--color-fg-muted)]">CPU %</dt>
										<dd class="font-mono text-xs">
											{cpuSeries[cpuSeries.length - 1]?.toFixed(1) ?? '—'}
										</dd>
									</div>
									<Sparkline points={cpuSeries} width={300} height={40} />
								</div>
								<div>
									<div class="flex items-baseline justify-between">
										<dt class="text-[var(--color-fg-muted)]">Memory (MiB)</dt>
										<dd class="font-mono text-xs">
											{memSeries[memSeries.length - 1]?.toFixed(0) ?? '—'}
										</dd>
									</div>
									<Sparkline
										points={memSeries}
										width={300}
										height={40}
										stroke="#8b5cf6"
										fill="rgba(139,92,246,0.10)"
									/>
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
			{#if deployments.length === 0}
				<EmptyState
					icon={Rocket}
					title="No deployments yet"
					description="Hit Deploy at the top to ship the current commit."
				/>
			{:else}
				<Card padded={false}>
					<table class="w-full text-sm">
						<thead
							class="text-left text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
						>
							<tr class="border-b border-[var(--color-border)]">
								<th class="px-5 py-2.5">#</th>
								<th class="px-5 py-2.5">Color</th>
								<th class="px-5 py-2.5">Status</th>
								<th class="px-5 py-2.5">Commit</th>
								<th class="px-5 py-2.5">Started</th>
								<th class="px-5 py-2.5">Failure</th>
								<th class="px-5 py-2.5"></th>
							</tr>
						</thead>
						<tbody>
							{#each deployments as d}
								<tr
									class="border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-bg-subtle)]"
								>
									<td class="px-5 py-2.5 font-mono">#{d.id}</td>
									<td class="px-5 py-2.5 text-[var(--color-fg-muted)]">{d.color}</td>
									<td class="px-5 py-2.5">
										<Badge tone={depStatusTone[d.status] ?? 'neutral'} size="sm">
											{d.status}
										</Badge>
									</td>
									<td class="px-5 py-2.5 font-mono text-xs text-[var(--color-fg-muted)]">
										{d.commitSha ? d.commitSha.slice(0, 7) : '—'}
									</td>
									<td class="px-5 py-2.5 text-xs text-[var(--color-fg-muted)]">
										{d.startedAt ? new Date(d.startedAt).toLocaleString() : '—'}
									</td>
									<td class="px-5 py-2.5 max-w-xs truncate text-xs text-[var(--color-danger)]">
										{d.failureReason || ''}
									</td>
									<td class="px-5 py-2.5 text-right">
										<button
											class="text-xs font-medium text-[var(--color-accent)] hover:underline"
											onclick={() => openDeployLog(d.id)}
										>
											View log
										</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</Card>
			{/if}
		{:else}
			<!-- Settings tab -->
			{#if revealedSecret}
				<Card title="Copy the webhook secret now — it will not be shown again">
					<div class="flex items-center gap-2">
						<code
							class="flex-1 break-all rounded-md bg-[var(--color-fg)] px-3 py-2 font-mono text-xs text-[var(--color-accent)]"
						>
							{revealedSecret}
						</code>
						<Button variant="secondary" onclick={() => copyToClipboard(revealedSecret!)}>
							<Copy class="h-4 w-4" />
							Copy
						</Button>
						<Button variant="ghost" onclick={() => (revealedSecret = null)}>Dismiss</Button>
					</div>
				</Card>
			{/if}
			{#if revealedPublicKey}
				<Card title="Copy the SSH public key into GitHub — paste it as a Deploy key">
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
					<div class="mt-3">
						<Button variant="ghost" onclick={() => (revealedPublicKey = null)}>Dismiss</Button>
					</div>
				</Card>
			{/if}

			<Card
				title="Routes"
				description="Give each service its own public hostname. Ports are auto-detected — only override when the probe gets it wrong."
			>
				{#if servicesLoading}
					<p class="text-sm text-[var(--color-fg-muted)]">Loading services…</p>
				{:else if servicesError}
					<p class="text-sm text-[var(--color-danger)]">{servicesError}</p>
				{:else if servicesSource === 'none'}
					<p
						class="rounded-md border border-[var(--color-border)] bg-[var(--color-bg-subtle)] px-3 py-2 text-sm text-[var(--color-fg-muted)]"
					>
						{servicesHint ||
							'No compose available yet — deploy at least once so Teal can list services.'}
					</p>
				{:else if services.length === 0}
					<p class="text-sm text-[var(--color-fg-muted)]">
						No services declared in the compose file.
					</p>
				{:else}
					<form
						onsubmit={(e) => {
							e.preventDefault();
							void saveRoutes();
						}}
						class="space-y-3"
					>
						<div class="overflow-hidden rounded-md border border-[var(--color-border)]">
							<table class="w-full text-sm">
								<thead
									class="bg-[var(--color-bg-subtle)] text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
								>
									<tr>
										<th class="px-3 py-2 text-left">Service</th>
										<th class="px-3 py-2 text-left">Domain</th>
										<th class="px-3 py-2 text-left w-32">Port</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-[var(--color-border)]">
									{#each services as svc (svc.name)}
										<tr>
											<td class="px-3 py-2 align-top">
												<div class="font-medium text-[var(--color-fg)]">{svc.name}</div>
												<div class="text-xs text-[var(--color-fg-muted)]">
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
							<div class="text-sm text-[var(--color-danger)]">{routesError}</div>
						{/if}
						<div class="flex items-center justify-between">
							<p class="text-xs text-[var(--color-fg-subtle)]">
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
						<label for="giturl" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Git URL
						</label>
						<Input
							id="giturl"
							bind:value={formGitUrl}
							placeholder="https://github.com/owner/repo.git"
						/>
						<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
							HTTPS or SSH. Leave empty to paste compose manually.
						</p>
					</div>
					<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<div>
							<label for="gitbranch" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
								Branch
							</label>
							<Input id="gitbranch" bind:value={formGitBranch} placeholder="main" />
						</div>
						<div>
							<label for="gitpath" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
								Compose path inside the repo
							</label>
							<Input
								id="gitpath"
								bind:value={formGitComposePath}
								placeholder="docker-compose.yml"
								mono
							/>
						</div>
					</div>
					<div>
						<label for="authkind" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Authentication
						</label>
						<Select id="authkind" bind:value={formGitAuthKind}>
							<option value="">Public (no auth)</option>
							<option value="ssh">SSH deploy key (Teal generates)</option>
							<option value="pat">Personal access token</option>
							<option value="github_app">GitHub App (recommended)</option>
						</Select>
					</div>
					{#if formGitAuthKind === 'github_app'}
						<div
							class="space-y-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-subtle)] p-3 text-sm"
						>
							{#if ghaReposLoading}
								<p class="text-[var(--color-fg-muted)]">Loading installations…</p>
							{:else if ghaReposError}
								<p class="text-[var(--color-danger)]">{ghaReposError}</p>
							{:else if !ghaRepos || !ghaRepos.configured}
								<p class="text-[var(--color-fg-muted)]">
									The platform GitHub App isn't configured yet. Set it up at
									<a class="text-[var(--color-accent)] underline" href="/settings/github-app">
										Settings → GitHub App
									</a>
									first (one-click flow available there), then come back here.
								</p>
							{:else if ghaRepos.installations.length === 0}
								<div class="space-y-2">
									<p class="text-[var(--color-fg-muted)]">
										The platform App isn't installed on any repos yet. Install it on at least
										one repo, then refresh.
									</p>
									{#if ghaRepos.appSlug}
										<Button
											size="sm"
											onclick={() =>
												window.open(
													`https://github.com/apps/${ghaRepos!.appSlug}/installations/new`,
													'_blank',
													'noopener'
												)}
										>
											Install on GitHub
										</Button>
									{/if}
								</div>
							{:else}
								<label
									for="repoPick"
									class="block text-xs font-medium text-[var(--color-fg-muted)]"
								>
									Pick a repo
								</label>
								<Select id="repoPick" bind:value={selectedRepoKey}>
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
								</Select>
								<div class="flex items-center justify-between gap-2">
									<p class="text-xs text-[var(--color-fg-subtle)]">
										Saving links this app to the picked repo + installation. The Git URL +
										auth fields are filled in for you.
									</p>
									<Button
										variant="secondary"
										size="sm"
										disabled={!selectedRepoKey || linkingRepo}
										onclick={() => void linkSelectedRepo()}
									>
										{linkingRepo ? 'Linking…' : 'Link repo'}
									</Button>
								</div>
								{#if ghaRepos.appSlug}
									<p class="text-xs text-[var(--color-fg-subtle)]">
										Don't see the repo you want?
										<a
											class="text-[var(--color-accent)] underline"
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
							<label
								for="credential"
								class="mb-1 block text-sm font-medium text-[var(--color-fg)]"
							>
								Personal access token
							</label>
							<Input
								id="credential"
								type="password"
								bind:value={formGitCredential}
								placeholder={app.hasGitCredential
									? '(leave empty to keep existing)'
									: 'ghp_…'}
							/>
						</div>
					{/if}
					<label class="flex items-center gap-2 text-sm text-[var(--color-fg)]">
						<input
							type="checkbox"
							bind:checked={formAutoDeploy}
							class="h-4 w-4 rounded border-[var(--color-border-strong)] text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
						/>
						Auto-deploy on webhook push to this branch
					</label>
					{#if settingsError}
						<div class="text-sm text-[var(--color-danger)]">{settingsError}</div>
					{/if}
					<div class="flex justify-end">
						<Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
					</div>
				</form>
			</Card>

			{#if app.gitAuthKind === 'ssh' && app.hasGitCredential && existingPublicKey}
				<Card
					title="SSH deploy key"
					description="Paste this public key into GitHub → repo → Settings → Deploy keys."
				>
					<div class="flex items-center gap-2">
						<code
							class="flex-1 break-all rounded-md bg-[var(--color-bg-subtle)] px-3 py-2 font-mono text-xs text-[var(--color-fg)]"
						>
							{existingPublicKey}
						</code>
						<Button
							variant="secondary"
							onclick={() => copyToClipboard(existingPublicKey!)}
						>
							<Copy class="h-4 w-4" />
							Copy
						</Button>
					</div>
					{#if existingFingerprint}
						<p class="mt-2 font-mono text-xs text-[var(--color-fg-muted)]">
							Fingerprint: {existingFingerprint}
						</p>
					{/if}
					<div class="mt-3 flex justify-end">
						<Button variant="ghost" size="sm" onclick={rotateDeployKey}>
							Rotate deploy key
						</Button>
					</div>
				</Card>
			{/if}

			{#if app.gitUrl && app.hasWebhookSecret}
				<Card
					title="Webhook"
					description="Configure this URL in GitHub → repo → Settings → Webhooks. Content type application/json. Secret is the value shown on initial save / rotate."
				>
					<div class="flex items-center gap-2">
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
					<div class="mt-3 flex justify-end">
						<Button variant="ghost" size="sm" onclick={rotateWebhookSecret}>
							Rotate webhook secret
						</Button>
					</div>
				</Card>
			{/if}

			<Card
				title="Resource limits"
				description="Applied to every service in this app's compose. Empty disables the limit."
			>
				<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
					<div>
						<label for="cpu" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							CPU
						</label>
						<Input id="cpu" bind:value={formCPULimit} placeholder="0.5" />
						<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
							Number of CPUs (e.g. 0.5, 2).
						</p>
					</div>
					<div>
						<label for="mem" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Memory
						</label>
						<Input id="mem" bind:value={formMemoryLimit} placeholder="512m" />
						<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
							Compose grammar (256m, 1g).
						</p>
					</div>
				</div>
			</Card>

			<Card
				title="Notifications"
				description="On every terminal deploy, Teal can POST a signed JSON event and email on failure."
			>
				<div class="space-y-3">
					<div>
						<label for="hook" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Outbound webhook URL
						</label>
						<Input
							id="hook"
							bind:value={formNotifyWebhookUrl}
							placeholder="https://hooks.example.com/teal"
						/>
					</div>
					{#if app.hasNotificationSecret}
						<div class="flex justify-end">
							<Button
								variant="ghost"
								size="sm"
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
						<label for="nemail" class="mb-1 block text-sm font-medium text-[var(--color-fg)]">
							Failure email recipient
						</label>
						<Input
							id="nemail"
							type="email"
							bind:value={formNotifyEmail}
							placeholder="ops@example.com"
						/>
						<p class="mt-1 text-xs text-[var(--color-fg-subtle)]">
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
		class="fixed inset-0 z-[1100] flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
		style="animation: overlay-in 120ms ease-out both;"
		onclick={(e) => {
			if (e.target === e.currentTarget) {
				logModalDepID = null;
				logModalText = '';
			}
		}}
		role="presentation"
	>
		<div
			class="flex max-h-[85vh] w-full max-w-4xl flex-col overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-modal)]"
			style="animation: dialog-in 160ms cubic-bezier(0.2, 0.9, 0.3, 1.1) both;"
			role="dialog"
			aria-modal="true"
		>
			<div
				class="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-3"
			>
				<h2 class="text-sm font-semibold text-[var(--color-fg)]">
					Deployment #{logModalDepID} log
				</h2>
				<button
					class="-m-1 rounded p-1 text-[var(--color-fg-subtle)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-fg)]"
					aria-label="Close"
					onclick={() => {
						logModalDepID = null;
						logModalText = '';
					}}
				>
					<X class="h-4 w-4" />
				</button>
			</div>
			<div
				class="flex-1 overflow-auto bg-[#0b0b0e] p-3 font-mono text-xs leading-relaxed text-zinc-100"
			>
				{#if logModalLoading}
					<div class="text-zinc-400">Loading…</div>
				{:else}
					<pre class="whitespace-pre-wrap break-all">{logModalText || '(empty)'}</pre>
				{/if}
			</div>
		</div>
	</div>
{/if}
