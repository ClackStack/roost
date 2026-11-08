import { readFileSync } from 'node:fs';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, type Plugin } from 'vite';

// Serves the installer at https://roost.clackstack.dev/install.sh, so the
// README's `curl | sh` command points at the site instead of
// raw.githubusercontent. scripts/ — the repo root — stays the single copy;
// the release assets stay pinned to their tags.
const installScripts = (): Plugin => ({
	name: 'install-scripts',
	buildStart() {
		if (this.environment?.config.build.ssr) return;
		this.emitFile({
			type: 'asset',
			fileName: 'install.sh',
			source: readFileSync(new URL('../install.sh', import.meta.url))
		});
	}
});

export default defineConfig({
	// Keep assets as files: the CSP below refuses data: URLs.
	build: { assetsInlineLimit: 0 },
	plugins: [
		installScripts(),
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
