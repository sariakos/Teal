// Theme store. Three states from the user's perspective:
//   - 'system' → follow prefers-color-scheme (default for new users)
//   - 'light' / 'dark' → explicit override, persisted to localStorage
//
// The active resolved value (what's on <html data-theme>) is always
// 'light' or 'dark'. The inline script in app.html reads localStorage
// directly so the first paint already has the right theme; this store
// then takes over for runtime toggling.

type Preference = 'system' | 'light' | 'dark';
type Resolved = 'light' | 'dark';

const STORAGE_KEY = 'teal:theme';

function readPreference(): Preference {
	if (typeof localStorage === 'undefined') return 'system';
	const v = localStorage.getItem(STORAGE_KEY);
	if (v === 'light' || v === 'dark') return v;
	return 'system';
}

function systemDark(): boolean {
	if (typeof window === 'undefined' || !window.matchMedia) return false;
	return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function resolve(pref: Preference): Resolved {
	if (pref === 'system') return systemDark() ? 'dark' : 'light';
	return pref;
}

function apply(resolved: Resolved) {
	if (typeof document === 'undefined') return;
	document.documentElement.setAttribute('data-theme', resolved);
}

class ThemeStore {
	preference = $state<Preference>('system');
	resolved = $state<Resolved>('light');

	init() {
		this.preference = readPreference();
		this.resolved = resolve(this.preference);
		apply(this.resolved);

		if (typeof window !== 'undefined' && window.matchMedia) {
			const mq = window.matchMedia('(prefers-color-scheme: dark)');
			mq.addEventListener('change', () => {
				if (this.preference === 'system') {
					this.resolved = systemDark() ? 'dark' : 'light';
					apply(this.resolved);
				}
			});
		}
	}

	set(pref: Preference) {
		this.preference = pref;
		this.resolved = resolve(pref);
		apply(this.resolved);
		if (typeof localStorage === 'undefined') return;
		if (pref === 'system') localStorage.removeItem(STORAGE_KEY);
		else localStorage.setItem(STORAGE_KEY, pref);
	}

	toggle() {
		// One-click toggle for the topbar button: just flip the resolved
		// value and pin it explicitly. No three-way cycle through 'system'
		// — most users just want light↔dark.
		this.set(this.resolved === 'dark' ? 'light' : 'dark');
	}
}

export const theme = new ThemeStore();
