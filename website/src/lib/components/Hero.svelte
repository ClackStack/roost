<script lang="ts">
	import Bird from './Bird.svelte';
	import Download from './Download.svelte';
</script>

<header class="top wrap">
	<a class="logo" href="#top" aria-label="Roost home"><Bird size={46} />roost</a>
	<nav aria-label="Main">
		<a href="#leaks">why</a>
		<a href="#search">search</a>
		<a href="#mcp">MCP</a>
		<a href="#compare">compare</a>
		<a href="#limits">limits</a>
		<a href="#get" class="get">download</a>
	</nav>
</header>

<main id="top" class="hero wrap">
	<h1>
		<span>Your docs stay home.</span>
		<span><mark class="hl">Your AI</mark> still reads them.</span>
	</h1>

	<ol class="panels">
		<li class="frame">
			<svg viewBox="0 -64 424 344" fill="none" aria-hidden="true">
				<g filter="url(#wob)" stroke="var(--ink)" stroke-width="4" stroke-linecap="round" stroke-linejoin="round">
					<path d="M214 16h110l30 30v176H214z" fill="var(--teal)" />
					<path d="M156 36h110l30 30v176H156z" fill="var(--yellow)" />
					<path d="M96 56h110l30 30v176H96z" fill="#fff" />
					<path d="M206 56v30h30" />
					<path d="M118 130h96M118 160h96M118 190h60" />
				</g>
				<text x="118" y="112" font-size="24">.docx</text>
				<text x="20" y="-22" font-size="24">some of it is secret-ish</text>
				<text x="240" y="118" font-size="19">.xlsx</text>
				<text x="302" y="84" font-size="19">.pdf</text>
			</svg>
			<p><span>1.</span> You have a pile of Word, Excel and PDF files.</p>
		</li>
		<li class="frame">
			<svg viewBox="0 -64 424 344" fill="none" aria-hidden="true">
				<g filter="url(#wob)" stroke="var(--ink)" stroke-width="4" stroke-linecap="round" stroke-linejoin="round">
					<path d="M46 226h200v34H46zM62 192h170v34H62zM78 160h140v32H78z" fill="#fff" />
					<rect x="284" y="36" width="118" height="84" rx="10" fill="var(--teal)" />
					<path d="M304 78q10-16 20 0t20 0 20 0" />
					<path class="draw" pathLength="1" d="M282 96q-40 6-74 36" stroke-dasharray="1" />
					<path d="M208 132l4-20M208 132l21-3" />
				</g>
				<g transform="translate(92 60) scale(1.02)"><Bird size={106} /></g>
				<text x="290" y="150" font-size="22">your AI tool</text>
				<text x="20" y="-22" font-size="24">“what’s the Q3 budget?”</text>
			</svg>
			<p><span>2.</span> Roost sits on them and answers over MCP and REST.</p>
		</li>
		<li class="frame">
			<svg viewBox="0 -64 424 344" fill="none" aria-hidden="true">
				<g filter="url(#wob)" stroke="var(--ink)" stroke-width="4" stroke-linecap="round" stroke-linejoin="round">
					<path d="M246 76c-10-40 50-60 70-30 30-20 76 10 56 50 30 30-10 70-40 50-30 20-90 0-86-70z" fill="#fff" />
					<path class="draw" pathLength="1" d="M40 240q90-20 186-130" />
					<path d="M226 110l-29 6M226 110l-4 30" />
					<path d="M80 80l150 150M230 80L80 230" stroke="var(--pink)" stroke-width="13" />
				</g>
				<text x="268" y="104" font-size="24">internet</text>
				<text x="20" y="-22" font-size="24">not even an update check</text>
			</svg>
			<p><span>3.</span> Nothing leaves. We traced it: 0 connections.</p>
		</li>
	</ol>

	<div class="bar">
		<Download id="platform-top" />
		<p>One static binary. No telemetry, no update checks, no model downloads.</p>
	</div>
</main>

<style>
	.top {
		position: sticky;
		top: 0;
		z-index: 10;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px 28px;
		flex-wrap: wrap;
		padding-block: 12px;
		background: url('../assets/toner.png') var(--paper);
		max-width: none;
	}
	.logo {
		display: flex;
		align-items: center;
		gap: 10px;
		font: 400 2rem/1 var(--marker);
		text-decoration: none;
	}
	nav {
		display: flex;
		flex-wrap: wrap;
		gap: 2px 26px;
		font: 400 1.3rem/1 var(--marker);
	}
	nav a {
		display: inline-flex;
		align-items: center;
		min-height: 44px;
		text-decoration: none;
		background: linear-gradient(transparent 62%, var(--yellow) 62% 92%, transparent 92%) no-repeat 0 0 / 0% 100%;
		transition: background-size 220ms cubic-bezier(0.16, 1, 0.3, 1);
	}
	nav a:hover,
	nav a.get {
		background-size: 100% 100%;
	}
	.hero {
		display: flex;
		flex-direction: column;
		gap: clamp(20px, 3.2vh, 34px);
		height: clamp(600px, calc(100svh - 72px), 1000px);
		padding-block: 8px 28px;
	}
	h1 {
		font-size: clamp(2.5rem, min(5.9vw, 11.5vh), 5.6rem);
		text-transform: uppercase;
		line-height: 1.04;
	}
	h1 span {
		display: block;
	}
	h1 mark {
		--hl: var(--pink);
		color: inherit;
		background: none;
		transform: rotate(-1deg);
	}
	h1 mark::before {
		animation: load-swipe 700ms 250ms cubic-bezier(0.16, 1, 0.3, 1) backwards;
	}
	@keyframes load-swipe {
		from {
			clip-path: inset(0 100% 0 0);
		}
		to {
			clip-path: inset(0);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		h1 mark::before {
			animation: none;
		}
	}
	.panels {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		grid-template-rows: minmax(0, 1fr);
		gap: clamp(14px, 2vw, 28px);
		flex: 1;
		min-height: 0;
	}
	.panels li {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		gap: 8px;
		padding: 16px 18px;
		background: var(--white);
		min-height: 0;
	}
	.panels svg {
		width: 100%;
		flex: 1;
		min-height: 0;
		height: 0;
	}
	.panels text {
		font-family: var(--marker);
		fill: var(--ink);
	}
	.panels p {
		margin: 0;
		font: 400 clamp(1.3rem, 1rem + 0.8vw, 1.7rem) / 1.15 var(--marker);
	}
	.bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 14px 28px;
	}
	.bar p {
		margin: 0;
		font-size: 0.95em;
		max-width: none;
		flex: 1 1 320px;
	}
	.bar :global(.dl) {
		flex: 0 1 640px;
	}
	@media (max-width: 860px) {
		.hero {
			height: auto;
		}
		.panels {
			grid-template-columns: 1fr;
			grid-template-rows: none;
		}
		.panels svg {
			height: 240px;
			flex: none;
		}
		h1 span {
			display: inline;
		}
		h1 span + span::before {
			content: ' ';
		}
		nav {
			font-size: 1.15rem;
			gap: 0 16px;
		}
		.top {
			position: static;
		}
	}
	@media (max-width: 520px) {
		nav {
			font-size: 1rem;
			gap: 0 10px;
		}
	}
</style>
