import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// Vite config for the Teal SPA.
//
// Dev server proxy: requests to /api/* and /healthz are forwarded to the
// running Go backend (default :3000). The cookie domain ends up the same
// (localhost) so session cookies set by the backend round-trip correctly.
// In production, the SPA is served by the Go binary from the same origin,
// so this proxy is irrelevant.

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		port: 5173,
		proxy: {
			'/api': {
				target: 'http://localhost:3000',
				changeOrigin: false
			},
			'/healthz': {
				target: 'http://localhost:3000',
				changeOrigin: false
			}
		}
	}
});
