import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	// Keep assets as files: the CSP below refuses data: URLs.
	build: { assetsInlineLimit: 0 },
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter(),
			// The page promises no third-party requests. This makes the browser enforce it.
			csp: {
				mode: 'hash',
				directives: {
					'default-src': ['none'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-inline'],
					'font-src': ['self'],
					'img-src': ['self'],
					'connect-src': ['self'],
					'base-uri': ['none'],
					'form-action': ['none']
				}
			}
		})
	]
});
