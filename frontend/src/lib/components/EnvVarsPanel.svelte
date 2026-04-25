<script lang="ts">
	/*
	 * Per-app env vars + shared-env allow-list, with masked-by-default values
	 * and a single "Reveal all" toggle that triggers an audited backend
	 * reveal. Used inside the App detail page.
	 */
	import { onMount } from 'svelte';
	import { ApiError } from '$lib/api/client';
	import { appEnvVarsApi, appSharedEnvVarsApi, requiredEnvVarsApi } from '$lib/api/envvars';
	import type { RequiredEnvVar } from '$lib/api/envvars';
	import type { AppSharedListing, EnvVarRow } from '$lib/api/types';
	import Button from './Button.svelte';
	import Card from './Card.svelte';
	import Input from './Input.svelte';
	import Badge from './Badge.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { dialog } from '$lib/stores/dialog.svelte';
	import { dirty } from '$lib/stores/dirty.svelte';
	import { Eye, EyeOff, Trash2, Plus } from '@lucide/svelte';

	let { slug }: { slug: string } = $props();

	let rows = $state<EnvVarRow[]>([]);
	let loading = $state(true);
	let revealed = $state(false);
	let listError = $state<string | null>(null);

	let newKey = $state('');
	let newValue = $state('');
	let saving = $state(false);
	let formError = $state<string | null>(null);

	// View toggle for the per-app card. "table" is the row-by-row UI;
	// "editor" is a .env-style textarea for bulk paste/edit. Both views
	// read/write the same `rows` set — switching doesn't lose data.
	let viewMode = $state<'table' | 'editor'>('table');
	// Editor draft: the textarea contents. Re-seeded from `rows` whenever
	// we switch to editor mode or finish a save.
	let editorDraft = $state('');
	let editorError = $state<string | null>(null);
	let editorSaving = $state(false);

	let sharedListing = $state<AppSharedListing>({ included: [], available: [] });
	let savingShared = $state(false);
	let sharedError = $state<string | null>(null);

	// "Required by compose" — discovered from the app's compose YAML.
	let required = $state<RequiredEnvVar[]>([]);
	let requiredSource = $state<'checkout' | 'stored' | 'none' | ''>('');
	let requiredHint = $state<string>('');
	let requiredError = $state<string | null>(null);
	let requiredLoading = $state(true);
	// Inline value inputs for missing/default vars: keyed by var name so
	// edits survive across re-fetches.
	let inlineValues = $state<Record<string, string>>({});
	let inlineSavingKey = $state<string | null>(null);

	async function reload(reveal = false) {
		loading = true;
		listError = null;
		try {
			rows = await appEnvVarsApi.list(slug, reveal);
			revealed = reveal;
		} catch (err) {
			listError = err instanceof Error ? err.message : 'Failed to load env vars';
		} finally {
			loading = false;
		}
	}

	async function reloadRequired() {
		requiredLoading = true;
		requiredError = null;
		try {
			const r = await requiredEnvVarsApi.list(slug);
			required = r.vars;
			requiredSource = r.source;
			requiredHint = r.hint ?? '';
		} catch (err) {
			requiredError = err instanceof ApiError ? err.message : 'Could not load required env vars';
		} finally {
			requiredLoading = false;
		}
	}

	async function setInline(name: string) {
		const value = (inlineValues[name] ?? '').trim();
		if (value === '') return;
		inlineSavingKey = name;
		try {
			await appEnvVarsApi.upsert(slug, name, value);
			delete inlineValues[name];
			inlineValues = { ...inlineValues };
			await Promise.all([reload(revealed), reloadRequired()]);
			dirty.mark(slug);
			toast.success(`${name} set`, { description: 'Redeploy to apply.' });
		} catch (err) {
			toast.error('Save failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			inlineSavingKey = null;
		}
	}

	async function claimShared(name: string) {
		const set = new Set(sharedListing.included);
		set.add(name);
		await persistShared([...set].sort());
		await reloadRequired();
	}

	async function reloadShared() {
		try {
			sharedListing = await appSharedEnvVarsApi.list(slug);
		} catch {
			// Non-fatal — leave the existing listing.
		}
	}

	async function addEnvVar() {
		if (!newKey) return;
		saving = true;
		formError = null;
		try {
			await appEnvVarsApi.upsert(slug, newKey, newValue);
			const k = newKey;
			newKey = '';
			newValue = '';
			await reload(revealed);
			dirty.mark(slug);
			toast.success(`${k} saved`, { description: 'Redeploy to apply.' });
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Save failed';
			toast.error('Save failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			saving = false;
		}
	}

	// --- editor-mode helpers ---

	// Render the current rows as a .env-style block. When values aren't
	// revealed we still emit KEY= lines (with empty placeholder) so the
	// user sees the full key list — they have to flip Reveal to actually
	// edit values. New rows the user pastes get saved verbatim.
	function rowsToDraft(): string {
		return rows
			.slice()
			.sort((a, b) => a.key.localeCompare(b.key))
			.map((r) => {
				if (revealed && r.value !== undefined) return `${r.key}=${r.value}`;
				if (r.hasValue) return `${r.key}=********`;
				return `${r.key}=`;
			})
			.join('\n');
	}

	async function switchToEditor() {
		// Editor is most useful with real values visible. If we're masked,
		// reveal first (audited) so the textarea isn't full of placeholders
		// the user can't edit meaningfully.
		if (!revealed) {
			await reload(true);
		}
		editorDraft = rowsToDraft();
		editorError = null;
		viewMode = 'editor';
	}

	function switchToTable() {
		viewMode = 'table';
		editorError = null;
	}

	// Parse a .env-style block into a key→value map. Same rules as
	// docker compose's --env-file:
	//   - blank lines and # comments are skipped
	//   - leading "export " is allowed and stripped
	//   - the value is the literal RHS of the FIRST '=' (no quote stripping)
	//   - key must match [A-Za-z_][A-Za-z0-9_]*
	// Returns either parsed map or an error string with line number.
	function parseDotEnv(src: string): { entries: Record<string, string>; error: string | null } {
		const entries: Record<string, string> = {};
		const lines = src.split('\n');
		const keyRe = /^[A-Za-z_][A-Za-z0-9_]*$/;
		for (let i = 0; i < lines.length; i++) {
			const raw = lines[i];
			const trimmed = raw.trim();
			if (trimmed === '' || trimmed.startsWith('#')) continue;
			let line = trimmed;
			if (line.startsWith('export ')) line = line.slice(7).trimStart();
			const eq = line.indexOf('=');
			if (eq < 0) {
				return { entries: {}, error: `line ${i + 1}: missing '=' (got "${trimmed}")` };
			}
			const key = line.slice(0, eq).trim();
			const value = line.slice(eq + 1);
			if (!keyRe.test(key)) {
				return { entries: {}, error: `line ${i + 1}: invalid key "${key}" (use [A-Za-z_][A-Za-z0-9_]*)` };
			}
			entries[key] = value;
		}
		return { entries, error: null };
	}

	async function saveEditor() {
		editorError = null;
		const parsed = parseDotEnv(editorDraft);
		if (parsed.error) {
			editorError = parsed.error;
			return;
		}
		// Diff against current rows.
		const next = parsed.entries;
		const currentByKey: Record<string, EnvVarRow> = {};
		for (const r of rows) currentByKey[r.key] = r;

		// "********" is the masked placeholder we put into the textarea
		// when reveal was off — treat it as "no change" so we don't
		// overwrite real values with literal asterisks. Drop those keys
		// from the upsert set (but keep them out of the delete set too —
		// they still exist).
		const upserts: Array<[string, string]> = [];
		for (const [k, v] of Object.entries(next)) {
			if (v === '********' && currentByKey[k]?.hasValue) continue;
			const cur = currentByKey[k];
			if (cur && cur.value !== undefined && cur.value === v) continue;
			upserts.push([k, v]);
		}
		const deletes: string[] = [];
		for (const k of Object.keys(currentByKey)) {
			if (!(k in next)) deletes.push(k);
		}

		if (upserts.length === 0 && deletes.length === 0) {
			editorError = 'No changes to save.';
			return;
		}
		if (deletes.length > 0) {
			const ok = await dialog.confirm({
				title: `Remove ${deletes.length} variable${deletes.length === 1 ? '' : 's'}?`,
				body: deletes.join(', '),
				tone: 'warning',
				confirmLabel: 'Remove'
			});
			if (!ok) return;
		}

		editorSaving = true;
		try {
			for (const [k, v] of upserts) {
				await appEnvVarsApi.upsert(slug, k, v);
			}
			for (const k of deletes) {
				await appEnvVarsApi.remove(slug, k);
			}
			await Promise.all([reload(revealed), reloadRequired()]);
			editorDraft = rowsToDraft();
			dirty.mark(slug);
			const verb =
				upserts.length && deletes.length
					? `${upserts.length} saved, ${deletes.length} removed`
					: upserts.length
						? `${upserts.length} saved`
						: `${deletes.length} removed`;
			toast.success(verb, { description: 'Redeploy to apply.' });
		} catch (err) {
			editorError = err instanceof ApiError ? err.message : 'Save failed';
			toast.error('Save failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			editorSaving = false;
		}
	}

	async function deleteEnvVar(key: string) {
		if (
			!(await dialog.confirm({
				title: `Delete ${key}?`,
				tone: 'danger',
				confirmLabel: 'Delete'
			}))
		)
			return;
		try {
			await appEnvVarsApi.remove(slug, key);
			await reload(revealed);
			dirty.mark(slug);
			toast.success(`${key} deleted`, { description: 'Redeploy to apply.' });
		} catch (err) {
			toast.error('Delete failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		}
	}

	function toggleSharedKey(key: string, include: boolean) {
		const set = new Set(sharedListing.included);
		if (include) set.add(key);
		else set.delete(key);
		void persistShared([...set].sort());
	}

	async function persistShared(keys: string[]) {
		savingShared = true;
		sharedError = null;
		try {
			await appSharedEnvVarsApi.set(slug, keys);
			await reloadShared();
			dirty.mark(slug);
		} catch (err) {
			sharedError = err instanceof ApiError ? err.message : 'Save failed';
			toast.error('Save failed', {
				description: err instanceof ApiError ? err.message : undefined
			});
		} finally {
			savingShared = false;
		}
	}

	onMount(async () => {
		await Promise.all([reload(false), reloadShared(), reloadRequired()]);
	});
</script>

<div class="space-y-6">
	<Card
		title="Required by compose"
		description="Every variable referenced in this app's compose. Set values here and they flow into both interpolation and the running containers."
	>
		{#if requiredLoading}
			<p class="text-sm text-[var(--color-fg-muted)]">Loading…</p>
		{:else if requiredError}
			<p class="text-sm text-[var(--color-danger)]">{requiredError}</p>
		{:else if requiredSource === 'none'}
			<p
				class="rounded-md border border-[var(--color-border)] bg-[var(--color-bg-subtle)] px-3 py-2 text-sm text-[var(--color-fg-muted)]"
			>
				{requiredHint || 'No compose available yet — deploy at least once so Teal can scan.'}
			</p>
		{:else if required.length === 0}
			<p class="text-sm text-[var(--color-fg-muted)]">
				This compose doesn't reference any env vars. Nothing to configure here.
			</p>
		{:else}
			<div class="overflow-hidden rounded-md border border-[var(--color-border)]">
				<table class="w-full text-sm">
					<thead
						class="bg-[var(--color-bg-subtle)] text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
					>
						<tr>
							<th class="px-3 py-2 text-left">Variable</th>
							<th class="px-3 py-2 text-left">Status</th>
							<th class="px-3 py-2 text-left">Value</th>
							<th class="px-3 py-2 text-right"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-[var(--color-border)]">
						{#each required as v (v.name)}
							<tr>
								<td class="px-3 py-2 align-top">
									<div class="font-mono text-[var(--color-fg)]">{v.name}</div>
									{#if v.sources.length > 0}
										<div
											class="mt-0.5 truncate text-xs text-[var(--color-fg-muted)]"
											title={v.sources.join('\n')}
										>
											{v.sources[0]}{v.sources.length > 1 ? ` +${v.sources.length - 1}` : ''}
										</div>
									{/if}
								</td>
								<td class="px-3 py-2 align-top">
									{#if v.status === 'set'}
										<Badge tone="success" size="sm">set</Badge>
									{:else if v.status === 'shared'}
										<Badge tone="info" size="sm">shared</Badge>
									{:else if v.status === 'default'}
										<Badge tone="neutral" size="sm">uses default</Badge>
									{:else if v.status === 'unclaimed'}
										<Badge tone="warning" size="sm">shared (not opted in)</Badge>
									{:else}
										<Badge tone="danger" size="sm">missing</Badge>
									{/if}
								</td>
								<td class="px-3 py-2 align-top">
									{#if v.status === 'missing' || v.status === 'default'}
										<Input
											size="sm"
											bind:value={inlineValues[v.name]}
											placeholder={v.defaultValue
												? `default: ${v.defaultValue}`
												: 'set value'}
										/>
									{:else if v.status === 'set'}
										<span class="text-xs text-[var(--color-fg-subtle)]">configured below</span>
									{:else if v.status === 'shared'}
										<span class="text-xs text-[var(--color-fg-subtle)]">from shared env</span>
									{:else}
										<span class="text-xs text-[var(--color-fg-subtle)]">
											platform-wide value exists
										</span>
									{/if}
								</td>
								<td class="px-3 py-2 text-right align-top">
									{#if v.status === 'missing' || v.status === 'default'}
										<Button
											variant="secondary"
											size="sm"
											disabled={inlineSavingKey === v.name ||
												!(inlineValues[v.name] ?? '').trim()}
											onclick={() => setInline(v.name)}
										>
											{inlineSavingKey === v.name ? 'Saving…' : 'Set'}
										</Button>
									{:else if v.status === 'unclaimed'}
										<Button variant="secondary" size="sm" onclick={() => claimShared(v.name)}>
											Opt in
										</Button>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="mt-2 text-xs text-[var(--color-fg-subtle)]">Source: {requiredSource}</p>
		{/if}
	</Card>

	<Card title="Per-app environment variables">
		{#snippet actions()}
			<Button variant="ghost" size="sm" onclick={() => reload(!revealed)}>
				{#if revealed}
					<EyeOff class="h-3.5 w-3.5" />
					Hide
				{:else}
					<Eye class="h-3.5 w-3.5" />
					Reveal (audited)
				{/if}
			</Button>
		{/snippet}
		{#if listError}
			<div class="text-sm text-[var(--color-danger)]">{listError}</div>
		{:else if loading}
			<div class="text-sm text-[var(--color-fg-muted)]">Loading…</div>
		{:else}
			<div class="mb-3 flex items-center justify-between gap-3">
				<p class="text-sm text-[var(--color-fg-muted)]">
					Injected into the container as KEY=VALUE on every deploy.
				</p>
				<div class="inline-flex gap-1 rounded-md bg-[var(--color-bg-subtle)] p-0.5 text-xs">
					<button
						type="button"
						class="rounded px-2.5 py-1 transition-colors {viewMode === 'table'
							? 'bg-[var(--color-surface)] text-[var(--color-fg)] shadow-[var(--shadow-xs)]'
							: 'text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]'}"
						onclick={switchToTable}
					>
						Table
					</button>
					<button
						type="button"
						class="rounded px-2.5 py-1 transition-colors {viewMode === 'editor'
							? 'bg-[var(--color-surface)] text-[var(--color-fg)] shadow-[var(--shadow-xs)]'
							: 'text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]'}"
						onclick={() => void switchToEditor()}
					>
						.env editor
					</button>
				</div>
			</div>

			{#if viewMode === 'editor'}
				<p class="mb-2 text-xs text-[var(--color-fg-subtle)]">
					Edit as a .env file. <code>KEY=value</code> per line; <code>#</code> comments;
					<code>********</code> placeholders are masked existing values — leave them or replace.
				</p>
				<textarea
					bind:value={editorDraft}
					rows={Math.max(8, editorDraft.split('\n').length + 1)}
					spellcheck="false"
					class="block w-full rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-fg)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
					placeholder={'DATABASE_URL=postgres://…\nAUTH_SECRET=…\nLOG_LEVEL=info'}
				></textarea>
				{#if editorError}
					<div class="mt-2 text-sm text-[var(--color-danger)]">{editorError}</div>
				{/if}
				<div class="mt-3 flex items-center justify-between">
					<p class="text-xs text-[var(--color-fg-subtle)]">
						{rows.length} key{rows.length === 1 ? '' : 's'} currently set.
					</p>
					<Button onclick={() => void saveEditor()} disabled={editorSaving}>
						{editorSaving ? 'Saving…' : 'Save changes'}
					</Button>
				</div>
			{:else if rows.length === 0}
				<p class="text-sm text-[var(--color-fg-muted)]">No env vars configured.</p>
			{:else}
				<div class="overflow-hidden rounded-md border border-[var(--color-border)]">
					<table class="w-full text-sm">
						<thead
							class="bg-[var(--color-bg-subtle)] text-[10px] font-semibold uppercase tracking-wider text-[var(--color-fg-subtle)]"
						>
							<tr>
								<th class="px-3 py-2 text-left">Key</th>
								<th class="px-3 py-2 text-left">Value</th>
								<th class="px-3 py-2 text-left">Updated</th>
								<th class="px-3 py-2 text-right"></th>
							</tr>
						</thead>
						<tbody class="divide-y divide-[var(--color-border)]">
							{#each rows as r}
								<tr>
									<td class="px-3 py-2 font-mono text-[var(--color-fg)]">{r.key}</td>
									<td class="px-3 py-2 font-mono text-xs">
										{#if revealed && r.value !== undefined}
											<code
												class="rounded bg-[var(--color-bg-subtle)] px-2 py-0.5 text-[var(--color-fg)]"
											>
												{r.value}
											</code>
										{:else if r.hasValue}
											<span class="text-[var(--color-fg-subtle)]">••••••</span>
										{:else}
											<span class="text-[var(--color-fg-subtle)]">(empty)</span>
										{/if}
									</td>
									<td class="px-3 py-2 text-xs text-[var(--color-fg-muted)]">
										{r.updatedAt ? new Date(r.updatedAt).toLocaleString() : '—'}
									</td>
									<td class="px-3 py-2 text-right">
										<button
											class="inline-flex items-center gap-1 text-xs font-medium text-[var(--color-danger)] hover:underline"
											onclick={() => deleteEnvVar(r.key)}
										>
											<Trash2 class="h-3.5 w-3.5" />
											Delete
										</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			{#if viewMode === 'table'}
				<form
					class="mt-4 grid grid-cols-1 items-end gap-3 sm:grid-cols-[1fr_2fr_auto]"
					onsubmit={(e) => {
						e.preventDefault();
						void addEnvVar();
					}}
				>
					<div>
						<label
							for="newkey"
							class="mb-1 block text-xs font-medium text-[var(--color-fg-muted)]"
						>
							Key
						</label>
						<Input id="newkey" bind:value={newKey} placeholder="DATABASE_URL" mono />
					</div>
					<div>
						<label
							for="newvalue"
							class="mb-1 block text-xs font-medium text-[var(--color-fg-muted)]"
						>
							Value
						</label>
						<Input id="newvalue" bind:value={newValue} placeholder="postgres://…" />
					</div>
					<Button type="submit" disabled={saving || !newKey}>
						<Plus class="h-4 w-4" />
						{saving ? 'Saving…' : 'Save'}
					</Button>
				</form>
				{#if formError}
					<div class="mt-2 text-sm text-[var(--color-danger)]">{formError}</div>
				{/if}
			{/if}
		{/if}
	</Card>

	<Card
		title="Shared env vars"
		description="Pick which platform-wide shared keys this app receives. Per-app keys shadow shared keys with the same name."
	>
		{#if sharedListing.available.length === 0}
			<p class="text-sm text-[var(--color-fg-muted)]">
				No shared env vars are defined yet (admin sets them at Settings → Shared env).
			</p>
		{:else}
			<div class="grid grid-cols-1 gap-1 text-sm sm:grid-cols-2">
				{#each sharedListing.available as key}
					<label
						for={`shared-${key}`}
						class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-subtle)]"
					>
						<input
							type="checkbox"
							id={`shared-${key}`}
							checked={sharedListing.included.includes(key)}
							onchange={(e) =>
								toggleSharedKey(key, (e.currentTarget as HTMLInputElement).checked)}
							disabled={savingShared}
							class="h-4 w-4 rounded border-[var(--color-border-strong)] text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
						/>
						<span class="font-mono text-[var(--color-fg)]">{key}</span>
					</label>
				{/each}
			</div>
			{#if sharedListing.included.some((k) => !sharedListing.available.includes(k))}
				<p class="mt-3 text-xs text-[var(--color-warning-soft-fg)]">
					{sharedListing.included.filter((k) => !sharedListing.available.includes(k)).length} opted-in
					key(s) no longer exist as shared vars; they'll be skipped on deploy.
				</p>
			{/if}
			{#if sharedError}
				<div class="mt-2 text-sm text-[var(--color-danger)]">{sharedError}</div>
			{/if}
		{/if}
	</Card>
</div>
