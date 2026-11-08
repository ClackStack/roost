<script lang="ts">
	import Bird from './Bird.svelte';

	const replies = [
		'Go on. Try to send it.',
		'Telemetry? There is none in the binary.',
		'Update check? Roost doesn’t have one.',
		'Model download? Keyword search doesn’t need a model.',
		'Usage stats? Nothing collects them.',
		'Still 0. You could stop now, or run the real test yourself.'
	];

	let attempts = $state(0);
	let flying = $state(false);
	let reply = $derived(replies[Math.min(attempts, replies.length - 1)]);
	let mood = $derived<'happy' | 'worried' | 'proud'>(
		flying ? 'worried' : attempts >= replies.length - 1 ? 'proud' : 'happy'
	);

	function send() {
		attempts += 1;
		flying = !matchMedia('(prefers-reduced-motion: reduce)').matches;
	}
</script>

<div class="toy frame">
	<div class="scene" aria-hidden="true">
		<svg class={['doc', flying && 'fly']} onanimationend={() => (flying = false)} width="62" height="76" viewBox="0 0 62 76" fill="none" stroke="var(--ink)" stroke-width="4" stroke-linejoin="round">
			<path d="M4 3h38l16 16v54H4z" fill="#fff" />
			<path d="M42 3v16h16" />
			<circle cx="22" cy="38" r="3.5" fill="var(--ink)" stroke="none" />
			<circle cx="40" cy="38" r="3.5" fill="var(--ink)" stroke="none" />
			<path d="M24 52q7 6 14 0" stroke-linecap="round" />
		</svg>
		<svg class="wall" width="56" height="170" viewBox="0 0 56 170" fill="none" stroke="var(--ink)" stroke-width="4" filter="url(#wob)">
			<path d="M4 4h48v162H4zM4 36h48M4 68h48M4 100h48M4 132h48M28 4v32M16 36v32M40 36v32M28 68v32M16 100v32M40 100v32M28 132v34" fill="var(--pink)" />
		</svg>
		<Bird {mood} size={84} class="guard" />
		<svg class="cloud" width="136" height="91" viewBox="0 0 150 100" fill="none" stroke="var(--ink)" stroke-width="4" filter="url(#wob)">
			<path d="M26 74C2 74 2 40 28 40c0-26 40-34 50-10 22-14 46 4 36 24 22 2 20 30-4 30z" fill="#fff" />
		</svg>
		<span class="net">internet</span>
	</div>

	<dl>
		<div><dt>Tries</dt><dd>{attempts}</dd></div>
		<div><dt>Connections made</dt><dd>0</dd></div>
	</dl>
	<p class="reply" aria-live="polite">{reply}</p>
	<button class="btn" type="button" onclick={send} disabled={flying}>Send it to the internet</button>
	<p class="note">This toy is a joke. The strace test next to it isn’t.</p>
</div>

<style>
	.toy {
		background: var(--white);
		padding: clamp(16px, 2.4vw, 28px);
		display: flex;
		flex-direction: column;
		gap: 12px;
		min-width: 0;
	}
	.scene {
		position: relative;
		height: 190px;
		display: flex;
		align-items: flex-end;
		overflow: hidden;
	}
	.doc {
		position: absolute;
		left: 2%;
		bottom: 8px;
	}
	.wall {
		position: absolute;
		left: 46%;
		bottom: 0;
	}
	.scene :global(.guard) {
		position: absolute;
		left: calc(46% - 76px);
		bottom: 0;
	}
	.cloud {
		position: absolute;
		right: 0;
		top: 16px;
	}
	.net {
		position: absolute;
		right: 38px;
		top: 62px;
		font: 400 0.8rem/1 var(--marker);
	}
	@media (max-width: 520px) {
		.cloud {
			width: 104px;
			height: 70px;
			top: 30px;
		}
		.net {
			right: 24px;
			top: 62px;
			font-size: 0.68rem;
		}
	}
	.fly {
		animation: rebound 900ms cubic-bezier(0.3, 0, 0.3, 1);
	}
	@keyframes rebound {
		45% {
			left: calc(46% - 66px);
			bottom: 110px;
			rotate: 20deg;
		}
		60% {
			rotate: -14deg;
		}
		100% {
			left: 2%;
			bottom: 8px;
			rotate: 0deg;
		}
	}
	dl {
		display: flex;
		gap: 32px;
		margin: 0;
	}
	dt {
		font-weight: 700;
	}
	dd {
		margin: 0;
		font: 400 3rem/1 var(--marker);
	}
	.reply {
		font: 400 1.35rem/1.2 var(--marker);
		min-height: 2.4em;
		margin: 0;
	}
	.note {
		margin: 0;
		color: var(--ink-soft);
		font-size: 0.95em;
	}
	.btn {
		align-self: flex-start;
	}
</style>
