<script lang="ts">
	type Mood = 'happy' | 'worried' | 'proud' | 'sad';
	let {
		mood = 'happy',
		size = 96,
		fill = 'var(--pink)',
		class: className = ''
	}: { mood?: Mood; size?: number; fill?: string; class?: string } = $props();
</script>

<svg
	class={className}
	width={size}
	height={size}
	viewBox="0 0 106 106"
	fill="none"
	aria-hidden="true"
	stroke="var(--ink)"
	stroke-width="5"
	stroke-linejoin="round"
	stroke-linecap="round"
>
	<path
		d="M18 62c0-26 18-44 40-44 12 0 20 6 24 14 2 16-2 21-5 27-5 21-18 27-32 27-16 0-27-9-27-24z"
		{fill}
	/>
	<!-- Feather strokes: a filled wing here reads as a grin. -->
	<path d="M30 50c-2 8 0 14 6 20M41 50c-2 8 0 13 5 18" stroke-width="4" />
	<path d="M50 86v12m-8 0h16M66 84v14m-8 0h16" />
	{#if mood === 'happy'}
		<circle cx="66" cy="32" r="5" fill="var(--ink)" stroke="none" />
		<path d="M80 30l17 8-15 7z" fill="var(--yellow)" stroke-width="4" />
	{:else if mood === 'proud'}
		<path d="M60 33q6-7 12 0" stroke-width="4" />
		<path d="M80 34l18-10-14 19z" fill="var(--pink)" stroke-width="4" />
		<path d="M50 8l-3-7M64 6l1-6M38 14l-6-5" stroke-width="4" />
	{:else if mood === 'worried'}
		<circle cx="65" cy="33" r="7" fill="#fff" stroke-width="3.5" />
		<circle cx="66" cy="33" r="2.5" fill="var(--ink)" stroke="none" />
		<path d="M56 19l15-4" stroke-width="4" />
		<path d="M81 30l15 2-13 5zM82 41l14 8-16-1z" fill="var(--yellow)" stroke-width="3.5" />
		<path d="M97 8c2 4 4 6 2 9s-6 1-6-2 2-4 4-7z" fill="var(--teal)" stroke-width="3" />
	{:else}
		<path d="M59 34q6 5 12 0" stroke-width="4" />
		<path d="M59 22l12 5" stroke-width="4" />
		<path d="M80 36l13 15-15-3z" fill="var(--yellow)" stroke-width="4" />
		<path d="M66 46c-3 5-4 7-2 9s6 0 5-3-1-4-3-6z" fill="var(--teal)" stroke-width="2.5" />
	{/if}
</svg>
