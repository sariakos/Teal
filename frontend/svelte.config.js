import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	compilerOptions: {
		runes: ({ filename }) => (filename.split(/[/\\]/).includes('node_modules') ? undefined : true)
	},
	kit: {
		// SPA mode: prerender every route to index.html and let the SvelteKit
		// router resolve paths client-side. Lets us serve the SPA from a
		// static file server (the embedded Go FS in production).
		adapter: adapter({
			fallback: 'index.html',
			strict: false
		}),
		prerender: {
			// We don't pre-render at build time — the SPA fetches data from
			// the API on mount. Disabling avoids spurious build-time fetches
			// against an API that isn't running.
			handleMissingId: 'ignore',
			handleHttpError: 'ignore'
		}
	}
};

export default config;
