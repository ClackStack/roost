<script lang="ts">
	let {
		lines,
		mark,
		label
	}: { lines: string[]; mark?: number; label: string } = $props();

	let status = $state<'idle' | 'copied' | 'failed'>('idle');

	async function copy() {
		try {
			await navigator.clipboard.writeText(lines.join('\n'));
			status = 'copied';
		} catch {
			status = 'failed';
		}
		setTimeout(() => (status = 'idle'), 2000);
	}
</script>

<figure class="frame">
	<figcaption>
		<span>{label}</span>
		<button type="button" onclick={copy}>
			{status === 'copied' ? 'Copied' : status === 'failed' ? 'Copy failed, select it instead' : 'Copy'}
		</button>
	</figcaption>
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<pre tabindex="0"><code
			>{#each lines as line, i (i)}<span class={['line', i === mark && 'on']}>{line || ' '}</span
				>{/each}</code
		></pre>
</figure>

<style>
	figure {
		margin: 0;
		background: var(--white);
		min-width: 0;
	}
	figcaption {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 1rem;
		padding: 0.5rem 1rem;
		border-bottom: 3px solid var(--ink);
		font-weight: 700;
	}
	button {
		font: inherit;
		font-size: 0.9em;
		background: none;
		border: 0;
		padding: 0.35rem 0.25rem;
		min-height: 44px;
		color: inherit;
		text-decoration: underline;
		text-underline-offset: 3px;
		cursor: pointer;
	}
	button:hover {
		text-decoration-color: var(--pink);
		text-decoration-thickness: 3px;
	}
	pre {
		margin: 0;
		padding: 1rem;
		overflow-x: auto;
		line-height: 1.6;
	}
	pre code {
		background: none;
		padding: 0;
		font-size: clamp(0.82rem, 0.78rem + 0.2vw, 0.95rem);
	}
	.line {
		display: block;
		width: max-content;
		min-width: 100%;
	}
	.on {
		background: var(--yellow);
	}
</style>
