// Per-app "config changed but not yet deployed" tracker.
//
// Calls that mutate runtime config (env var save, settings PATCH, route
// add/remove) call dirty.mark(slug). A successful deploy calls
// dirty.clear(slug). The UI reads dirty.has(slug) to render a subtle
// indicator next to the app's name in the sidebar / app header.
//
// State is in-memory only — it's a hint, not a source of truth. After a
// reload we lose the dirty signal; that's acceptable since the user
// would have to navigate back to the app anyway and the indicator just
// nudges them to redeploy. We don't try to actually diff committed vs
// pending state because the source of truth is split across env vars,
// settings, routes, etc., and that level of fidelity isn't needed.

class DirtyStore {
	private slugs = $state<Set<string>>(new Set());

	mark(slug: string) {
		if (this.slugs.has(slug)) return;
		const next = new Set(this.slugs);
		next.add(slug);
		this.slugs = next;
	}

	clear(slug: string) {
		if (!this.slugs.has(slug)) return;
		const next = new Set(this.slugs);
		next.delete(slug);
		this.slugs = next;
	}

	has(slug: string): boolean {
		return this.slugs.has(slug);
	}

	get all(): readonly string[] {
		return Array.from(this.slugs);
	}
}

export const dirty = new DirtyStore();
