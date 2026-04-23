/*
 * Typed fetch wrapper around the Teal API.
 *
 * What it does:
 *   - Targets /api/v1 with credentials: 'include' so the session cookie
 *     round-trips on every request.
 *   - Reads the teal_csrf cookie and injects X-Csrf-Token on every unsafe
 *     method automatically — callers never deal with CSRF.
 *   - Throws ApiError on non-2xx responses with status + parsed body.
 *
 * What it does NOT do:
 *   - Cache. Pages are responsible for re-fetching when state changes.
 *   - Retry. The backend is local; transient failures are real failures.
 */

const API_BASE = '/api/v1';
const CSRF_COOKIE = 'teal_csrf';
const CSRF_HEADER = 'X-Csrf-Token';

const UNSAFE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

export class ApiError extends Error {
	constructor(
		public readonly status: number,
		message: string,
		public readonly body?: unknown
	) {
		super(message);
	}
}

function readCookie(name: string): string | null {
	if (typeof document === 'undefined') return null;
	const prefix = name + '=';
	for (const part of document.cookie.split('; ')) {
		if (part.startsWith(prefix)) return decodeURIComponent(part.slice(prefix.length));
	}
	return null;
}

interface RequestOptions {
	method?: string;
	body?: unknown;
	signal?: AbortSignal;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
	const method = (opts.method ?? 'GET').toUpperCase();
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
	if (UNSAFE_METHODS.has(method)) {
		const csrf = readCookie(CSRF_COOKIE);
		if (csrf) headers[CSRF_HEADER] = csrf;
	}

	const res = await fetch(API_BASE + path, {
		method,
		headers,
		credentials: 'include',
		body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
		signal: opts.signal
	});

	if (res.status === 204) return undefined as unknown as T;

	const text = await res.text();
	let parsed: unknown = undefined;
	if (text) {
		try {
			parsed = JSON.parse(text);
		} catch {
			parsed = text;
		}
	}

	if (!res.ok) {
		const msg =
			parsed && typeof parsed === 'object' && 'error' in parsed
				? String((parsed as { error: unknown }).error)
				: `request failed (${res.status})`;
		throw new ApiError(res.status, msg, parsed);
	}
	return parsed as T;
}

export const api = {
	get: <T>(path: string, signal?: AbortSignal) => request<T>(path, { signal }),
	post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body }),
	put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body }),
	patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body }),
	delete: <T>(path: string) => request<T>(path, { method: 'DELETE' })
};
