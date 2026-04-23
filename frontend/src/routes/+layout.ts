// Disable SSR — we ship as a SPA. This file is what tells SvelteKit not to
// try to render pages on the server side at build time.
export const ssr = false;

// Don't pre-render any individual page either. The fallback (`index.html`)
// is enough; the client-side router takes over from there.
export const prerender = false;

// Trailing-slash policy: keep them off so /api/v1/foo and /foo don't differ.
export const trailingSlash = 'never';
