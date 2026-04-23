/*
 * Authentication store. Holds the currently authenticated user (or null)
 * across the app. Uses Svelte 5 runes via $state.
 *
 * Loaded once in the root +layout.svelte; pages read directly. Mutating
 * actions (login, logout) update the store synchronously after the network
 * call returns.
 */

import type { User } from '$lib/api/types';

interface AuthState {
	user: User | null;
	loaded: boolean; // false until the first /me call resolves
}

function createAuthStore() {
	const state = $state<AuthState>({ user: null, loaded: false });
	return {
		get user() {
			return state.user;
		},
		get loaded() {
			return state.loaded;
		},
		set(user: User | null) {
			state.user = user;
			state.loaded = true;
		},
		clear() {
			state.user = null;
			state.loaded = true;
		}
	};
}

export const auth = createAuthStore();
