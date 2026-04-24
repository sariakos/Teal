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
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Save failed');
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
			newKey = '';
			newValue = '';
			await reload(revealed);
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Save failed';
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
			const ok = confirm(
				`Removing ${deletes.length} key(s): ${deletes.join(', ')}. Continue?`
			);
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
		} catch (err) {
			editorError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			editorSaving = false;
		}
	}

	async function deleteEnvVar(key: string) {
		if (!confirm(`Delete env var "${key}"?`)) return;
		try {
			await appEnvVarsApi.remove(slug, key);
			await reload(revealed);
		} catch (err) {
			alert(err instanceof ApiError ? err.message : 'Delete failed');
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
		} catch (err) {
			sharedError = err instanceof ApiError ? err.message : 'Save failed';
		} finally {
			savingShared = false;
		}
	}

	onMount(async () => {
		await Promise.all([reload(false), reloadShared(), reloadRequired()]);
	});
</script>

<div class="space-y-6">
	<Card title="Required by compose">
		<p class="mb-3 text-sm text-zinc-500">
			Every variable referenced in this app's compose file. Set values here and they flow into
			both <code>docker compose</code> interpolation and the running containers.
		</p>
		{#if requiredLoading}
			<p class="text-sm text-zinc-500">Loading…</p>
		{:else if requiredError}
			<p class="text-sm text-red-600">{requiredError}</p>
		{:else if requiredSource === 'none'}
			<p class="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-600">
				{requiredHint || 'No compose available yet — deploy at least once so Teal can scan.'}
			</p>
		{:else if required.length === 0}
			<p class="text-sm text-zinc-500">
				This compose doesn't reference any env vars. Nothing to configure here.
			</p>
		{:else}
			<div class="overflow-hidden rounded-md border border-zinc-200">
				<table class="w-full text-sm">
					<thead class="bg-zinc-50 text-xs uppercase text-zinc-500">
						<tr>
							<th class="px-3 py-2 text-left">Variable</th>
							<th class="px-3 py-2 text-left">Status</th>
							<th class="px-3 py-2 text-left">Value</th>
							<th class="px-3 py-2 text-right"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-100">
						{#each required as v (v.name)}
							<tr>
								<td class="px-3 py-2 align-top">
									<div class="font-mono text-zinc-800">{v.name}</div>
									{#if v.sources.length > 0}
										<div class="mt-0.5 truncate text-xs text-zinc-500" title={v.sources.join('\n')}>
											{v.sources[0]}{v.sources.length > 1 ? ` +${v.sources.length - 1}` : ''}
										</div>
									{/if}
								</td>
								<td class="px-3 py-2 align-top">
									{#if v.status === 'set'}
										<span class="rounded bg-teal-100 px-2 py-0.5 text-xs font-medium text-teal-800">set</span>
									{:else if v.status === 'shared'}
										<span class="rounded bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-800">shared</span>
									{:else if v.status === 'default'}
										<span class="rounded bg-zinc-100 px-2 py-0.5 text-xs font-medium text-zinc-700">uses default</span>
									{:else if v.status === 'unclaimed'}
										<span class="rounded bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800">shared (not opted in)</span>
									{:else}
										<span class="rounded bg-red-100 px-2 py-0.5 text-xs font-medium text-red-800">missing</span>
									{/if}
								</td>
								<td class="px-3 py-2 align-top">
									{#if v.status === 'missing' || v.status === 'default'}
										<Input
											bind:value={inlineValues[v.name]}
											placeholder={v.defaultValue ? `default: ${v.defaultValue}` : 'set value'}
										/>
									{:else if v.status === 'set'}
										<span class="text-xs text-zinc-500">configured below</span>
									{:else if v.status === 'shared'}
										<span class="text-xs text-zinc-500">from shared env</span>
									{:else}
										<span class="text-xs text-zinc-500">platform-wide value exists</span>
									{/if}
								</td>
								<td class="px-3 py-2 text-right align-top">
									{#if v.status === 'missing' || v.status === 'default'}
										<Button
											variant="secondary"
											disabled={inlineSavingKey === v.name || !(inlineValues[v.name] ?? '').trim()}
											onclick={() => setInline(v.name)}
										>
											{inlineSavingKey === v.name ? 'Saving…' : 'Set'}
										</Button>
									{:else if v.status === 'unclaimed'}
										<Button variant="secondary" onclick={() => claimShared(v.name)}>
											Opt in
										</Button>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="mt-2 text-xs text-zinc-500">Source: {requiredSource}</p>
		{/if}
	</Card>

	<Card title="Per-app environment variables">
		{#if listError}
			<div class="text-sm text-red-600">{listError}</div>
		{:else if loading}
			<div class="text-sm text-zinc-500">Loading…</div>
		{:else}
			<div class="mb-3 flex items-center justify-between gap-3">
				<p class="text-sm text-zinc-500">
					Injected into the container as KEY=VALUE on every deploy.
				</p>
				<div class="flex items-center gap-2">
					<div class="inline-flex overflow-hidden rounded-md border border-zinc-300 text-xs">
						<button
							type="button"
							class="px-3 py-1.5 {viewMode === 'table'
								? 'bg-zinc-100 text-zinc-900'
								: 'bg-white text-zinc-500 hover:text-zinc-800'}"
							onclick={switchToTable}
						>
							Table
						</button>
						<button
							type="button"
							class="border-l border-zinc-300 px-3 py-1.5 {viewMode === 'editor'
								? 'bg-zinc-100 text-zinc-900'
								: 'bg-white text-zinc-500 hover:text-zinc-800'}"
							onclick={() => void switchToEditor()}
						>
							.env editor
						</button>
					</div>
					<Button variant="secondary" onclick={() => reload(!revealed)}>
						{revealed ? 'Hide values' : 'Reveal values (audited)'}
					</Button>
				</div>
			</div>

			{#if viewMode === 'editor'}
				<p class="mb-2 text-xs text-zinc-500">
					Edit as a .env file. Lines like <code>KEY=value</code>; comments with <code>#</code>;
					blank lines ignored. Values rendered as <code>********</code> are masked
					placeholders — leave them as-is to keep the existing value, or replace with a new one.
				</p>
				<textarea
					bind:value={editorDraft}
					rows={Math.max(8, editorDraft.split('\n').length + 1)}
					spellcheck="false"
					class="block w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs text-zinc-800 focus:border-teal-500 focus:outline-none focus:ring-1 focus:ring-teal-500"
					placeholder={'DATABASE_URL=postgres://…\nAUTH_SECRET=…\nLOG_LEVEL=info'}
				></textarea>
				{#if editorError}
					<div class="mt-2 text-sm text-red-600">{editorError}</div>
				{/if}
				<div class="mt-3 flex items-center justify-between">
					<p class="text-xs text-zinc-500">
						{rows.length} key{rows.length === 1 ? '' : 's'} currently set. Saving diffs against the
						current set: missing keys get deleted (with confirm), changed values get upserted.
					</p>
					<Button
						onclick={() => void saveEditor()}
						disabled={editorSaving}
					>
						{editorSaving ? 'Saving…' : 'Save changes'}
					</Button>
				</div>
			{:else if rows.length === 0}
				<p class="text-sm text-zinc-500">No env vars configured.</p>
			{:else}
				<table class="w-full text-sm">
					<thead class="text-left text-xs uppercase text-zinc-500">
						<tr>
							<th class="pb-2">Key</th>
							<th class="pb-2">Value</th>
							<th class="pb-2">Updated</th>
							<th class="pb-2"></th>
						</tr>
					</thead>
					<tbody>
						{#each rows as r}
							<tr class="border-t border-zinc-100">
								<td class="py-2 font-mono">{r.key}</td>
								<td class="py-2 font-mono text-xs">
									{#if revealed && r.value !== undefined}
										<code class="rounded bg-zinc-50 px-2 py-0.5 text-zinc-800">{r.value}</code>
									{:else if r.hasValue}
										<span class="text-zinc-400">••••••</span>
									{:else}
										<span class="text-zinc-400">(empty)</span>
									{/if}
								</td>
								<td class="py-2 text-zinc-500">
									{r.updatedAt ? new Date(r.updatedAt).toLocaleString() : '—'}
								</td>
								<td class="py-2 text-right">
									<button
										class="text-sm text-red-600 hover:underline"
										onclick={() => deleteEnvVar(r.key)}
									>
										Delete
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}

			{#if viewMode === 'table'}
				<form
					class="mt-4 grid grid-cols-[1fr_2fr_auto] items-end gap-2"
					onsubmit={(e) => {
						e.preventDefault();
						void addEnvVar();
					}}
				>
					<div>
						<label for="newkey" class="mb-1 block text-xs text-zinc-500">Key</label>
						<Input id="newkey" bind:value={newKey} placeholder="DATABASE_URL" />
					</div>
					<div>
						<label for="newvalue" class="mb-1 block text-xs text-zinc-500">Value</label>
						<Input id="newvalue" bind:value={newValue} placeholder="postgres://…" />
					</div>
					<Button type="submit" disabled={saving || !newKey}>
						{saving ? 'Saving…' : 'Add / Update'}
					</Button>
				</form>
				{#if formError}
					<div class="mt-2 text-sm text-red-600">{formError}</div>
				{/if}
			{/if}
		{/if}
	</Card>

	<Card title="Shared env vars">
		<p class="mb-3 text-sm text-zinc-500">
			Pick which platform-wide shared keys this app should receive. Per-app keys above shadow
			shared keys with the same name.
		</p>
		{#if sharedListing.available.length === 0}
			<p class="text-sm text-zinc-500">No shared env vars are defined yet (admin sets them).</p>
		{:else}
			<ul class="space-y-1 text-sm">
				{#each sharedListing.available as key}
					<li class="flex items-center gap-2">
						<input
							type="checkbox"
							id={`shared-${key}`}
							checked={sharedListing.included.includes(key)}
							onchange={(e) =>
								toggleSharedKey(key, (e.currentTarget as HTMLInputElement).checked)}
							disabled={savingShared}
						/>
						<label class="font-mono" for={`shared-${key}`}>{key}</label>
					</li>
				{/each}
			</ul>
			{#if sharedListing.included.some((k) => !sharedListing.available.includes(k))}
				<p class="mt-3 text-sm text-amber-700">
					{sharedListing.included.filter((k) => !sharedListing.available.includes(k)).length} opted-in
					key(s) no longer exist as shared vars; they'll be skipped on deploy.
				</p>
			{/if}
			{#if sharedError}
				<div class="mt-2 text-sm text-red-600">{sharedError}</div>
			{/if}
		{/if}
	</Card>
</div>
