<script lang="ts">
	import type { Snippet } from 'svelte';
	let {
		color,
		id,
		label,
		tilt = '-0.4deg',
		children
	}: { color: string; id?: string; label: string; tilt?: string; children: Snippet } = $props();
</script>

<!-- A coloured sheet stapled in between the paper pages. -->
<section {id} aria-label={label} style:--sheet={color} style:--tilt={tilt} class="insert">
	<svg class="staple a" width="44" height="12" viewBox="0 0 44 12" fill="none" aria-hidden="true">
		<path d="M3 9V4h38v5" stroke="var(--ink)" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" />
	</svg>
	<svg class="staple b" width="44" height="12" viewBox="0 0 44 12" fill="none" aria-hidden="true">
		<path d="M3 9V4h38v5" stroke="var(--ink)" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" />
	</svg>
	<div class="in">
		{@render children()}
	</div>
</section>

<style>
	.insert {
		position: relative;
		margin-block: 24px;
		isolation: isolate;
		overflow-x: clip;
		overflow-y: visible;
	}
	/* The sheet tilts; the text on it doesn't, so it stays crisp. */
	.insert::before {
		content: '';
		position: absolute;
		inset: 0 -3%;
		z-index: -1;
		background: url('../assets/toner.png'), radial-gradient(rgb(17 17 17 / 13%) 1.6px, transparent 2px) 0 0 / 15px 15px, var(--sheet);
		transform: rotate(var(--tilt));
		box-shadow: 0 3px 14px rgb(17 17 17 / 28%);
	}
	.staple {
		position: absolute;
		top: 2px;
		rotate: -3deg;
	}
	.a {
		left: 14%;
	}
	.b {
		right: 16%;
		rotate: 4deg;
	}
</style>
