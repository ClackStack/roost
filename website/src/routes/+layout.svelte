<script lang="ts">
	import '@fontsource/permanent-marker/latin-400.css';
	import '@fontsource/karla/latin-500.css';
	import '@fontsource/karla/latin-700.css';
	import '@fontsource/jetbrains-mono/latin-400.css';
	import '../app.css';

	let { children } = $props();
</script>

<svelte:head>
	<meta property="og:site_name" content="Roost" />
	<meta property="og:image" content="https://roost.clackstack.dev/og.png" />
	<meta property="og:image:width" content="1200" />
	<meta property="og:image:height" content="630" />
	<meta property="og:image:alt" content="The Roost site: a self-hosted document server for AI tools." />
	<meta property="og:locale" content="en_US" />
	<meta name="twitter:card" content="summary_large_image" />
	<script type="application/ld+json">
		{
			"@context": "https://schema.org",
			"@type": "Organization",
			"name": "Roost",
			"url": "https://roost.clackstack.dev/",
			"logo": "https://roost.clackstack.dev/favicon.svg",
			"sameAs": ["https://github.com/ClackStack/roost"]
		}
	</script>
</svelte:head>

<svg class="defs" width="0" height="0" aria-hidden="true">
	<filter id="wob">
		<feTurbulence baseFrequency="0.035" numOctaves="2" seed="4" />
		<feDisplacementMap in="SourceGraphic" scale="4" />
	</filter>
</svg>

{@render children()}

<style>
	.defs {
		position: absolute;
	}
</style>
