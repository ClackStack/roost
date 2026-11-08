<script lang="ts">
	import Hero from '#lib/components/Hero.svelte';
	import Bird from '#lib/components/Bird.svelte';
	import Insert from '#lib/components/Insert.svelte';
	import Code from '#lib/components/Code.svelte';
	import Download from '#lib/components/Download.svelte';
	import LeakToy from '#lib/components/LeakToy.svelte';
	import SearchToy from '#lib/components/SearchToy.svelte';
	import { checksumsUrl, exampleConfigUrl, licence, readmeUrl, repo, securityUrl } from '#lib/site.ts';

	const timings = [
		{ what: 'A rare word', ms: 21, p95: null },
		{ what: 'Two words', ms: 21, p95: '146 ms' },
		{ what: 'A six-word question', ms: 320, p95: '880 ms' },
		{ what: 'A word in nearly every chunk', ms: 710, p95: '1 s' }
	];

	type Tab = { id: string; label: string; note: string; scrawl?: string; lines: string[] };

	const tabs: Tab[] = [
		{
			id: 'quack',
			label: 'MCP in quack',
			note: 'For quack, a terminal coding agent from the same author.',
			lines: [
				'# ~/.config/quack/mcp.d/roost.toml',
				'url = "https://roost.internal:8484/mcp"',
				'headers = { Authorization = { env = "ROOST_TOKEN", format = "Bearer {}" } }',
				'[tools]',
				'auto_approve = ["list_*", "search_*", "get_*", "read_*"]'
			]
		},
		{
			id: 'http',
			label: 'MCP over HTTP',
			note: 'For a shared server. This example is for Claude Code.',
			lines: [
				'claude mcp add --transport http roost \\',
				'  https://roost.internal:8484/mcp \\',
				'  --header "Authorization: Bearer $TOKEN"'
			]
		},
		{
			id: 'stdio',
			label: 'MCP over stdio',
			note: 'For a client on the same machine as the index.',
			scrawl: 'Server tested with raw JSON-RPC. This config hasn’t met a real app yet.',
			lines: [
				'{',
				'  "mcpServers": {',
				'    "roost": {',
				'      "command": "/usr/local/bin/roost",',
				'      "args": ["mcp", "--config", "/etc/roost/roost.toml"],',
				'      "env": { "ROOST_TOKEN": "roost_..." }',
				'    }',
				'  }',
				'}'
			]
		},
		{
			id: 'rest',
			label: 'REST',
			note: 'For RAG pipelines and scripts. The spec is at /openapi.json.',
			scrawl: 'This route is covered by make e2e, with curl.',
			lines: [
				'curl -H "Authorization: Bearer $TOKEN" \\',
				'  -d \'{"query":"acquisition timeline"}\' \\',
				'  http://127.0.0.1:8484/v1/search'
			]
		}
	];
	let tab = $state('quack');
	let current = $derived(tabs.find((t) => t.id === tab) ?? tabs[0]);

	const tools = [
		['list_collections', 'What can this token read?'],
		['search_documents', 'Ranked snippets, filtered by collection, type, path or date.'],
		['list_documents', 'Browse files, including ones Roost couldn’t read, and why.'],
		['get_document_info', 'Metadata and an outline of sections.'],
		['read_document', 'A whole document or one section, in pages.']
	];

	const rows = [
		['Outbound calls', 'None by default, traced with strace', 'None mentioned; local by default', 'Optional model download at setup'],
		['Word, Excel, PowerPoint', 'Yes', 'No, text files only', 'Not listed'],
		['PDF', 'Yes, built in', 'No, binaries are excluded', 'Yes, best with pdftotext installed'],
		['Separate access per user', 'Yes, per token and collection', 'Scoped tokens, not multi-tenant', 'On its roadmap'],
		['Log of every access', 'Yes', 'Not listed', 'Not listed'],
		['MCP', 'HTTP and stdio', 'Yes, HTTP and a stdio bridge', 'Yes, HTTP API is now its main route'],
		['What you run', 'One static binary', 'Python 3.11 or 3.12, plus ripgrep', 'A Rust binary'],
		['Semantic search', 'Opt-in, needs your own embeddings service', 'No, by design', 'Built in, after a model download of about 90 MB'],
		['Indexes source code', 'No', 'Some: .py, .ts and .tsx only', 'Yes, with tree-sitter'],
		['Pulls from URLs on request', 'No', 'Not listed', 'Not listed']
	];

	const boring = [
		['Tokens that mean it', 'Each token sees only the collections you granted. The filter runs inside the SQL query, and a file you can’t see looks exactly like one that doesn’t exist.', 'M6 26a9 9 0 1 0 18 0a9 9 0 1 0-18 0M24 26h22M38 26v9M45 26v7'],
		['An audit log with teeth', 'Every access is logged. If the log can’t be written, the request is refused.', 'M10 6h30v40H10zM18 6v40M24 16h10M24 24h10M24 32h6'],
		['Files are treated as hostile', 'Parsers run in limited child processes. A nasty PDF kills one worker, not the server.', 'M12 12h20l8 8v26H12zM32 12v8h8M20 28l4 4M24 28l-4 4M30 28l4 4M34 28l-4 4M21 40q6-5 12 0'],
		['Answers you can check', 'Every result names the page, slide, sheet or heading it came from.', 'M10 6h24l8 8v32H10zM34 6v8h8M18 28l7 7 12-14'],
		['Tests that were tested', 'We broke 17 security controls on purpose. The test suite caught all 17.', 'M26 6l6 13 14 2-10 10 2 14-12-7-12 7 2-14L6 21l14-2z']
	];

	const cant = [
		'Read scanned PDFs. There’s no OCR yet.',
		'Read old .doc, .xls or .ppt files. Convert them first.',
		'Read email attachments.',
		'Do SSO. Authentication is by API token.',
		'Run on more than one node.',
		'Follow NTFS or SharePoint permissions. Access is per collection.',
		'Encrypt its index. Put it on an encrypted volume.'
	];

	const untested = [
		'MCP client apps, including Claude Code. We used the official Go MCP client, raw JSON-RPC and curl.',
		'Running on Windows and macOS. The binaries compile but haven’t been run.',
		'Real NFS and SMB shares.',
		'The Docker image, which hasn’t been built yet.'
	];
</script>

<svelte:head>
	<title>Roost: your docs stay home, your AI still reads them</title>
	<meta
		name="description"
		content="Roost is a self-hosted document server for AI tools. One static binary indexes Word, Excel, PowerPoint and PDF files and serves them over MCP and REST, with no outbound calls by default."
	/>
</svelte:head>

<Hero />

<section id="leaks" class="page wrap leaks" aria-labelledby="leaks-h">
	<div class="intro">
		<h2 id="leaks-h">Nothing leaves. <mark class="hl swipe">Not even a little.</mark></h2>
		<p class="lede">
			Don’t take a cartoon bird’s word for it. Here’s a toy to play with, and the real test to run.
		</p>
	</div>
	<LeakToy />
	<div class="proof">
		<ol>
			<li><strong>The build checks.</strong> <code>egress_test.go</code> fails if our code makes outbound calls anywhere outside one opt-in package.</li>
			<li><strong>The binary gets traced.</strong> <code>make e2e</code> runs the real thing under strace through indexing, serving and stdio. It made 0 network connections, across 52 passing checks.</li>
			<li><strong>The trace gets tested too.</strong> With embeddings on, the same trace does see connections, and only to the endpoint you configured.</li>
		</ol>
		<Code label="Check it yourself, from a source checkout" lines={['make e2e   # needs strace, curl, openssl, python3']} mark={0} />
		<div class="exception frame">
			<Bird mood="worried" size={72} />
			<p>
				<strong>The one exception:</strong> semantic search sends document text to an embeddings service you choose. It’s off by default and only connects to private addresses unless you override that. <a href={securityUrl}>Read SECURITY.md</a>.
			</p>
		</div>
	</div>
</section>

<Insert color="var(--yellow)" id="search" label="Search speed">
	<div class="page wrap speed">
		<div class="intro">
			<h2>Quick on most searches. Honest about the slow ones.</h2>
			<p class="lede">50,000 files, 300,000 chunks, one ordinary 6-core i5. These are median times.</p>
		</div>
		<div class="numbers">
			<ul class="chart">
				{#each timings as t (t.what)}
					<li>
						<span class="what">{t.what}</span>
						<span class="row">
							<span class="bar" style:--w={t.ms / 710}></span>
							<b>{t.ms} ms</b>
						</span>
						{#if t.p95}<span class="p95">95th percentile: {t.p95}</span>{/if}
					</li>
				{/each}
			</ul>
			<div class="margin-note">
				<svg width="74" height="52" viewBox="0 0 74 52" fill="none" stroke="var(--ink)" stroke-width="3.5" stroke-linecap="round" aria-hidden="true">
					<g filter="url(#wob)">
						<path class="draw" pathLength="1" d="M68 46C40 46 20 34 12 8" />
						<path d="M12 8l-7 16M12 8l14 10" />
					</g>
				</svg>
				<p class="scrawl">That’s the worst case. We show it anyway.</p>
			</div>
			<p>
				The whole corpus indexed in 1m58s, peaking at 46 MB of memory. It was plain text, so real Office and PDF files will take longer. Search time grows with the number of matching chunks.
			</p>
		</div>
		<SearchToy />
	</div>
</Insert>

<section id="mcp" class="page wrap mcp" aria-labelledby="mcp-h">
	<div>
		<h2 id="mcp-h">Speaks MCP. And REST, for <mark class="hl swipe">everything else.</mark></h2>
		<p class="lede">
			Same token, same access rules and same audit log either way. Every tool is read-only, so an agent can look but can’t touch.
		</p>
		<ul class="tools">
			{#each tools as [name, what] (name)}
				<li><code>{name}</code><span>{what}</span></li>
			{/each}
		</ul>
	</div>
	<div class="connect">
		<div class="tabs" role="group" aria-label="Connection type">
			{#each tabs as t (t.id)}
				<button type="button" aria-pressed={tab === t.id} onclick={() => (tab = t.id)}>{t.label}</button>
			{/each}
		</div>
		<p>{current.note}</p>
		<Code label={current.label} lines={current.lines} />
		{#if current.scrawl}
			<svg class="arrow" viewBox="0 0 220 110" fill="none" stroke="var(--ink)" stroke-width="4" stroke-linecap="round" aria-hidden="true">
				<g filter="url(#wob)">
					<path class="draw" pathLength="1" d="M204 98C150 104 70 90 26 22" />
					<path d="M26 22l-3 28M26 22l26 12" />
				</g>
			</svg>
			<p class="scrawl">{current.scrawl}</p>
		{/if}
	</div>
</section>

<section id="compare" class="page wrap compare" aria-labelledby="compare-h">
	<h2 id="compare-h">So why not use <span class="ul">something else?</span></h2>
	<p class="lede">
		You might! These are good projects, built for individual developers and their code and notes. Roost is for office files and a roomful of people.
	</p>
	<div class="table frame">
		<table>
			<thead>
				<tr>
					<th scope="col"><span class="sr">Feature</span></th>
					<th scope="col" class="us">Roost</th>
					<th scope="col"><a href="https://github.com/ctxd-dev/ctxd">ctxd</a></th>
					<th scope="col"><a href="https://github.com/epicsagas/alcove">alcove</a></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as [what, roost, ctxd, alcove] (what)}
					<tr>
						<th scope="row">{what}</th>
						<td class="us" data-who="Roost">{roost}</td>
						<td data-who="ctxd">{ctxd}</td>
						<td data-who="alcove">{alcove}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	<p class="fine">Checked against each project’s README and docs in November 2026. “Not listed” means they don’t mention it, not that it can’t be done.</p>
</section>

<Insert color="var(--teal)" label="Security and operations" tilt="0.3deg">
	<div class="page wrap boring">
		<div class="bhead"><h2>The boring bits, done properly.</h2><Bird mood="proud" size={110} fill="var(--yellow)" /></div>
		<dl>
			{#each boring as [title, text, icon] (title)}
				<div>
					<svg width="52" height="52" viewBox="0 0 52 52" fill="none" stroke="var(--ink)" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
						<g filter="url(#wob)"><path d={icon} /></g>
					</svg>
					<dt>{title}</dt>
					<dd>{text}</dd>
				</div>
			{/each}
		</dl>
	</div>
</Insert>

<section id="limits" class="page wrap limits" aria-labelledby="limits-h">
	<div class="head">
		<Bird mood="sad" size={120} />
		<h2 id="limits-h">Things Roost can’t do. <mark class="hl swipe">Yet.</mark></h2>
	</div>
	<div class="cols">
		<div>
			<h3>It can’t</h3>
			<ul>{#each cant as c (c)}<li>{c}</li>{/each}</ul>
		</div>
		<div>
			<h3>We haven’t tested</h3>
			<ul class="q">{#each untested as c (c)}<li>{c}</li>{/each}</ul>
			<p>Anything Roost can’t read shows up in <code>roost status</code>. It never drops a file silently.</p>
		</div>
	</div>
</section>

<Insert color="var(--pink)" id="get" label="Download Roost" tilt="0.35deg">
	<div class="page wrap get">
		<div>
			<h2>Still here? Grab the binary.</h2>
			<p class="lede">No installer, no sign-up, no account. One file on a machine inside your network.</p>
			<Download id="platform-bottom" />
			<p class="small">
				<a href={checksumsUrl}>SHA-256 checksums</a> · <a href={readmeUrl}>Full docs</a> · Air-gapped? <code>make offline-bundle</code> packages the source with every dependency.
			</p>
		</div>
		<div>
			<Code
				label="Quick start, Linux and macOS"
				mark={4}
				lines={[
					'mv roost-* roost && chmod +x roost',
					'cp roost.example.toml roost.toml',
					'# set data_dir and collections in roost.toml',
					'./roost token create --name alice --collections legal,hr',
					'./roost serve'
				]}
			/>
			<p class="small">Download <a href={exampleConfigUrl}>roost.example.toml</a> first, it isn’t inside the binary. Save the token, it’s shown only once.</p>
		</div>
	</div>
</Insert>

<footer class="wrap">
	<p><Bird size={40} /> <strong>This page made 0 third-party requests.</strong> Open your network tab and check.</p>
	<p><a href={repo}>Source</a> · {licence}</p>
</footer>

<style>
	.leaks {
		display: grid;
		grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
		gap: 28px clamp(28px, 5vw, 72px);
		align-items: start;
	}
	.leaks .intro {
		grid-column: 1 / -1;
	}
	.proof ol {
		margin: 0 0 20px;
		padding-left: 1.4em;
	}
	.proof li {
		margin-bottom: 0.8em;
	}
	.proof li::marker {
		font-family: var(--marker);
		font-size: 1.2em;
	}
	.exception {
		display: flex;
		gap: 14px;
		align-items: center;
		margin-top: 20px;
		padding: 14px 18px;
		background: var(--white);
	}
	.exception p {
		margin: 0;
	}
	.exception :global(svg) {
		flex: none;
	}

	.speed .intro {
		grid-column: 1 / -1;
	}
	.speed {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		gap: clamp(28px, 5vw, 72px);
		align-items: start;
		padding-block: clamp(40px, 6vw, 88px);
	}
	.chart {
		list-style: none;
		margin: 0 0 24px;
		padding: 0;
	}
	.chart li {
		margin-bottom: 16px;
	}
	.what {
		display: block;
		font-weight: 700;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.bar {
		height: 28px;
		flex: 0 1 calc(var(--w) * 78%);
		min-width: 12px;
		background: var(--pink);
		mask: url('../lib/assets/swipe.svg') center / 100% 100% no-repeat;
	}
	.margin-note {
		display: flex;
		align-items: flex-end;
		gap: 10px;
		margin: -14px 0 22px 34%;
	}
	.margin-note p {
		margin: 0;
	}
	.row b {
		font: 400 1.6rem/1 var(--marker);
		white-space: nowrap;
	}
	.p95 {
		font-size: 0.95em;
	}

	.mcp {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		gap: clamp(28px, 5vw, 72px);
	}
	.tools {
		list-style: none;
		margin: 24px 0 0;
		padding: 0;
	}
	.tools li {
		display: grid;
		grid-template-columns: 13.5em 1fr;
		gap: 2px 14px;
		padding: 10px 0;
		border-top: 2px dashed var(--ink);
	}
	.tools code {
		background: none;
		padding: 0;
	}
	.connect {
		min-width: 0;
		position: relative;
	}
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-bottom: 14px;
	}
	.tabs button {
		font: 400 1.15rem/1 var(--marker);
		min-height: 48px;
		padding: 0 14px;
		color: inherit;
		background: var(--white);
		border: 3px solid var(--ink);
		border-radius: 12px 8px 14px 7px;
		cursor: pointer;
	}
	.tabs button:hover {
		background: var(--paper);
		transform: rotate(-1deg);
	}
	.tabs button[aria-pressed='true'] {
		background: var(--teal);
	}
	.arrow {
		display: block;
		width: 180px;
		margin: 10px 0 0 40px;
	}
	.scrawl {
		font: 400 1.35rem/1.15 var(--marker);
		margin: 0 0 0 150px;
		max-width: 18em;
	}

	.compare {
		padding-top: 0;
	}
	.table {
		background: var(--white);
	}
	table {
		width: 100%;
		border-collapse: collapse;
		table-layout: fixed;
	}
	th,
	td {
		text-align: left;
		vertical-align: top;
		padding: 12px 16px;
		border-bottom: 2px dashed var(--ink);
	}
	tbody tr:last-child > * {
		border-bottom: 0;
	}
	thead th {
		font: 400 clamp(0.9rem, 0.6rem + 0.8vw, 1.35rem)/1.15 var(--marker);
		white-space: nowrap;
		padding-inline: 12px;
		border-bottom: 3px solid var(--ink);
	}
	tbody th {
		font-weight: 700;
	}
	.us {
		background: var(--yellow);
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}
	.fine {
		margin-top: 12px;
		color: var(--ink-soft);
		font-size: 0.95em;
	}

	.boring {
		padding-block: clamp(40px, 6vw, 88px);
	}
	.bhead {
		display: flex;
		align-items: center;
		gap: 18px;
	}
	.bhead :global(svg) {
		flex: none;
	}
	.boring dl {
		columns: 2 22em;
		column-gap: clamp(28px, 5vw, 72px);
		margin: 20px 0 0;
	}
	.boring dl div {
		break-inside: avoid;
		margin-bottom: 26px;
		display: grid;
		grid-template-columns: 52px 1fr;
		column-gap: 14px;
	}
	.boring dl svg {
		grid-row: span 2;
	}
	.boring dd {
		grid-column: 2;
	}
	.boring dt {
		font: 400 1.6rem/1.15 var(--marker);
	}
	.boring dd {
		margin: 4px 0 0;
		max-width: 52ch;
	}

	.head {
		display: flex;
		align-items: center;
		gap: 18px;
		margin-bottom: 20px;
	}
	.head h2 {
		margin: 0;
	}
	.head :global(svg) {
		flex: none;
	}
	.limits .hl {
		--hl: var(--pink);
	}
	.cols {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		gap: clamp(28px, 5vw, 72px);
	}
	.cols ul {
		margin: 0 0 1em;
		padding-left: 1.4em;
		list-style-image: url('../lib/assets/x.svg');
	}
	.cols .q {
		list-style-image: url('../lib/assets/q.svg');
	}
	.cols li {
		margin-bottom: 0.45em;
	}

	.get {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		gap: clamp(28px, 5vw, 72px);
		align-items: center;
		padding-block: clamp(40px, 6vw, 88px);
	}
	.small {
		margin-top: 14px;
		font-size: 0.98em;
	}

	footer {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 8px 24px;
		padding-block: 28px;
	}
	footer p {
		margin: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		max-width: none;
	}

	@media (max-width: 960px) {
		.leaks,
		.speed,
		.mcp,
		.cols,
		.get {
			grid-template-columns: minmax(0, 1fr);
		}
		.tools li {
			grid-template-columns: 1fr;
		}
		.scrawl {
			margin-left: 0;
		}
		.arrow {
			margin-left: 8px;
			width: 120px;
		}
	}

	@media (max-width: 720px) {
		thead {
			position: absolute;
			width: 1px;
			height: 1px;
			overflow: hidden;
			clip-path: inset(50%);
		}
		table,
		tbody,
		tr,
		th,
		td {
			display: block;
		}
		tbody tr {
			padding: 12px 16px;
			border-bottom: 3px solid var(--ink);
		}
		tbody tr:last-child {
			border-bottom: 0;
		}
		tbody th {
			padding: 0 0 6px;
			border: 0;
			font: 400 1.35rem/1.15 var(--marker);
		}
		td {
			padding: 4px 8px;
			border: 0;
			display: grid;
			grid-template-columns: 8.5em 1fr;
			gap: 10px;
		}
		td::before {
			content: attr(data-who);
			font-weight: 700;
			overflow-wrap: anywhere;
		}
	}
</style>
