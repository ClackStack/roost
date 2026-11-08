<script lang="ts">
	import { onMount } from 'svelte';
	import { platforms, downloadUrl } from '#lib/site.ts';

	let { id }: { id: string } = $props();

	let chosen = $state(platforms[0].id);
	let platform = $derived(platforms.find((p) => p.id === chosen) ?? platforms[0]);

	// Runs in the browser only and sends nothing anywhere.
	onMount(() => {
		const ua = navigator.userAgent;
		if (/Windows/.test(ua)) chosen = 'windows-amd64';
		else if (/Mac OS X/.test(ua)) chosen = 'darwin-arm64';
		else if (/aarch64|arm64/i.test(ua)) chosen = 'linux-arm64';
	});
</script>

<div class="dl">
	<label class="pick frame">
		<span class="sr">Choose your platform</span>
		<select {id} bind:value={chosen}>
			{#each platforms as p (p.id)}
				<option value={p.id}>{p.label}</option>
			{/each}
		</select>
		<svg width="18" height="12" viewBox="0 0 20 12" fill="none" aria-hidden="true">
			<path d="M2 2l8 8 8-8" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" />
		</svg>
	</label>
	<a class="btn" href={downloadUrl(platform)}>Download Roost · about 16 MiB</a>
</div>

<style>
	.dl {
		display: flex;
		gap: 12px;
		align-items: stretch;
	}
	.pick {
		position: relative;
		display: flex;
		align-items: center;
		background: var(--white);
		min-height: 52px;
		flex: 1 1 220px;
		max-width: 300px;
	}
	.pick svg {
		position: absolute;
		right: 16px;
		pointer-events: none;
	}
	select {
		appearance: none;
		width: 100%;
		height: 100%;
		border: 0;
		background: none;
		color: inherit;
		font: 700 1.05rem var(--body);
		padding: 0 42px 0 16px;
		border-radius: inherit;
		cursor: pointer;
	}
	.btn {
		flex: 1 1 auto;
		white-space: nowrap;
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
	@media (max-width: 520px) {
		.dl {
			flex-wrap: wrap;
		}
		.pick {
			max-width: none;
			flex-basis: 100%;
		}
		.btn {
			font-size: 1.15rem;
		}
	}
</style>
