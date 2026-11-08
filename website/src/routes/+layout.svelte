<script lang="ts">
	import '@fontsource/permanent-marker/latin-400.css';
	import '@fontsource/karla/latin-500.css';
	import '@fontsource/karla/latin-700.css';
	import '@fontsource/jetbrains-mono/latin-400.css';
	import '../app.css';

	let { children } = $props();
</script>

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
