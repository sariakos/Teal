<!--
  Platform settings (admin only). Edits the KV table on the backend; ACME
  changes prompt the user to restart Traefik because static config is read
  at boot.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { ApiError } from '$lib/api/client';
	import {
		settingsApi,
		SETTING_ACME_EMAIL,
		SETTING_ACME_STAGING,
		SETTING_HTTPS_REDIRECT,
		SETTING_SMTP_HOST,
		SETTING_SMTP_PORT,
		SETTING_SMTP_USER,
		SETTING_SMTP_PASS,
		SETTING_SMTP_FROM,
		SETTING_SMTP_STARTTLS
	} from '$lib/api/settings';
	import { platformApi } from '$lib/api/notifications';
	import type { PlatformSetting } from '$lib/api/types';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Input from '$lib/components/Input.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';

	let acmeEmail = $state('');
	let acmeStaging = $state(false);
	let httpsRedirect = $state(false);
	let smtpHost = $state('');
	let smtpPort = $state('587');
	let smtpUser = $state('');
	let smtpPass = $state('');
	let smtpFrom = $state('');
	let smtpStartTLS = $state(true);
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let restartHint = $state(false);
	let updateRequested = $state(false);

	function applyRows(rows: PlatformSetting[]) {
		const map = new Map(rows.map((r) => [r.key, r.value]));
		acmeEmail = map.get(SETTING_ACME_EMAIL) ?? '';
		acmeStaging = map.get(SETTING_ACME_STAGING) === 'true';
		httpsRedirect = map.get(SETTING_HTTPS_REDIRECT) === 'true';
		smtpHost = map.get(SETTING_SMTP_HOST) ?? '';
		smtpPort = map.get(SETTING_SMTP_PORT) ?? '587';
		smtpUser = map.get(SETTING_SMTP_USER) ?? '';
		// Password is stored as plain TEXT in the KV — surface it so admins
		// can rotate. (This is the correct trade-off for a single-host
		// platform with one admin role; the row IS the secret store.)
		smtpPass = map.get(SETTING_SMTP_PASS) ?? '';
		smtpFrom = map.get(SETTING_SMTP_FROM) ?? '';
		smtpStartTLS = (map.get(SETTING_SMTP_STARTTLS) ?? 'true') === 'true';
	}

	async function reload() {
		loading = true;
		error = null;
		try {
			applyRows(await settingsApi.list());
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load settings';
		} finally {
			loading = false;
		}
	}

	async function save() {
		saving = true;
		error = null;
		restartHint = false;
		try {
			// Updating each key independently so the backend can flag exactly
			// which ones touched the static config. Bool keys store "true"/"false"
			// to match what the engine reads back.
			const r1 = await settingsApi.upsert(SETTING_ACME_EMAIL, acmeEmail);
			const r2 = await settingsApi.upsert(SETTING_ACME_STAGING, acmeStaging ? 'true' : 'false');
			const r3 = await settingsApi.upsert(
				SETTING_HTTPS_REDIRECT,
				httpsRedirect ? 'true' : 'false'
			);
			await Promise.all([
				settingsApi.upsert(SETTING_SMTP_HOST, smtpHost),
				settingsApi.upsert(SETTING_SMTP_PORT, smtpPort),
				settingsApi.upsert(SETTING_SMTP_USER, smtpUser),
				settingsApi.upsert(SETTING_SMTP_PASS, smtpPass),
				settingsApi.upsert(SETTING_SMTP_FROM, smtpFrom || smtpUser),
				settingsApi.upsert(SETTING_SMTP_STARTTLS, smtpStartTLS ? 'true' : 'false')
			]);
			restartHint = r1.restartTraefik || r2.restartTraefik || r3.restartTraefik;
			await reload();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function selfUpdate() {
		const typed = await dialog.prompt({
			title: 'Update Teal platform?',
			body: 'Pulls the latest backend image, restarts the container. There will be a brief outage of the Teal UI; running app deploys finish independently.',
			tone: 'warning',
			expect: 'update-platform',
			placeholder: 'update-platform',
			help: 'Type the phrase to confirm.',
			confirmLabel: 'Update'
		});
		if (typed !== 'update-platform') return;
		try {
			const r = await platformApi.selfUpdate();
			updateRequested = true;
			toast.info(r.message, { duration: 8000 });
		} catch (err) {
			toast.error('Self-update failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	onMount(reload);
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">Platform settings</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Affect every app served by this Teal instance. Admin only.
		</p>
	</div>

	<Card title="HTTPS / ACME">
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
				<div>
					<label for="acme-email" class="mb-1 block text-sm font-medium text-zinc-700">
						ACME registration email
					</label>
					<Input
						id="acme-email"
						bind:value={acmeEmail}
						placeholder="ops@example.com"
					/>
					<p class="mt-1 text-xs text-zinc-500">
						Required by Let's Encrypt. Setting any value here also enables the HTTPS entrypoint.
					</p>
				</div>
				<label class="flex items-center gap-2 text-sm text-zinc-700">
					<input type="checkbox" bind:checked={acmeStaging} />
					Use Let's Encrypt staging (test certs; no rate limits)
				</label>
				<label class="flex items-center gap-2 text-sm text-zinc-700">
					<input type="checkbox" bind:checked={httpsRedirect} />
					Redirect plain HTTP to HTTPS (308) for every app
				</label>
				{#if restartHint}
					<div class="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-800">
						ACME changes affect Traefik's static config. Restart the Traefik container so the new
						config is loaded:
						<code class="mt-1 block font-mono text-xs">docker compose restart traefik</code>
					</div>
				{/if}
				{#if error}
					<div class="text-sm text-red-600">{error}</div>
				{/if}
				<div class="flex justify-end">
					<Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
				</div>
			</form>
		{/if}
	</Card>

	<Card title="SMTP (failure emails)">
		<p class="mb-3 text-sm text-zinc-500">
			Apps with a notification email get a message when a deploy fails. Leave the host empty to
			disable email entirely.
		</p>
		<form
			class="space-y-3"
			onsubmit={(e) => {
				e.preventDefault();
				void save();
			}}
		>
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label for="smtphost" class="mb-1 block text-sm font-medium text-zinc-700">Host</label>
					<Input id="smtphost" bind:value={smtpHost} placeholder="smtp.example.com" />
				</div>
				<div>
					<label for="smtpport" class="mb-1 block text-sm font-medium text-zinc-700">Port</label>
					<Input id="smtpport" bind:value={smtpPort} placeholder="587" />
				</div>
				<div>
					<label for="smtpuser" class="mb-1 block text-sm font-medium text-zinc-700">User</label>
					<Input id="smtpuser" bind:value={smtpUser} />
				</div>
				<div>
					<label for="smtppass" class="mb-1 block text-sm font-medium text-zinc-700">Password</label>
					<Input id="smtppass" type="password" bind:value={smtpPass} />
				</div>
				<div class="col-span-2">
					<label for="smtpfrom" class="mb-1 block text-sm font-medium text-zinc-700">From</label>
					<Input id="smtpfrom" bind:value={smtpFrom} placeholder="teal@example.com" />
				</div>
			</div>
			<label class="flex items-center gap-2 text-sm text-zinc-700">
				<input type="checkbox" bind:checked={smtpStartTLS} />
				Use STARTTLS
			</label>
			<div class="flex justify-end">
				<Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
			</div>
		</form>
	</Card>

	<Card title="Update platform">
		<p class="mb-3 text-sm text-zinc-500">
			Writes a restart marker and exits the Teal process. Your supervisor (systemd / docker
			<code>restart: unless-stopped</code>) starts the new image. Pull the latest tag first
			(<code>docker compose pull</code>).
		</p>
		<div class="flex justify-end">
			<Button variant="danger" onclick={selfUpdate} disabled={updateRequested}>
				{updateRequested ? 'Restarting…' : 'Update platform'}
			</Button>
		</div>
	</Card>
</div>
