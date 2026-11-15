<script lang="ts">
	type TourTab = {
		id: 'picker' | 'token' | 'dashboard' | 'guards';
		label: string;
		tag: string;
	};

	const tourTabs: TourTab[] = [
		{ id: 'picker', label: '1. Pick folders', tag: 'Wizard · Collections' },
		{ id: 'token', label: '2. First token', tag: 'Wizard · Finish' },
		{ id: 'dashboard', label: '3. 6 live tabs', tag: 'Existing config' },
		{ id: 'guards', label: '4. Safe saves', tag: 'Guards & defaults' }
	];

	let activeTab = $state<TourTab['id']>('picker');

	// Tab 1: interactive folder picker preview
	let showHidden = $state(false);
	let pickedFolders = $state<string[]>(['legal']);
	let attemptedOverlap = $state(false);

	const exampleFolders = $derived([
		{ name: 'legal', path: '/srv/documents/legal', hidden: false, blocked: false, note: 'collection' },
		{ name: 'hr', path: '/srv/documents/hr', hidden: false, blocked: false, note: 'collection' },
		{ name: 'roost-data', path: '/srv/roost-data', hidden: false, blocked: true, note: 'storage.data_dir' },
		...(showHidden
			? [{ name: '.archive', path: '/srv/documents/.archive', hidden: true, blocked: false, note: 'hidden' }]
			: [])
	]);

	function toggleFolder(name: string, blocked: boolean) {
		if (blocked) {
			attemptedOverlap = !attemptedOverlap;
			return;
		}
		attemptedOverlap = false;
		if (pickedFolders.includes(name)) {
			pickedFolders = pickedFolders.filter((item) => item !== name);
		} else {
			pickedFolders = [...pickedFolders, name];
		}
	}

	// Tab 2: first token + snippet preview
	let tokenScope = $state<'scoped' | 'all'>('scoped');
	let snippetKind = $state<'quack' | 'mcp' | 'rest'>('quack');

	const scopeLabel = $derived(tokenScope === 'scoped' ? 'legal, hr' : '* (all collections)');

	const snippetText = $derived.by(() => {
		if (snippetKind === 'quack') {
			return [
				'# ~/.config/quack/mcp.d/roost.toml',
				'url = "http://127.0.0.1:8484/mcp"',
				'headers = { Authorization = { env = "ROOST_TOKEN", format = "Bearer {}" } }'
			].join('\n');
		}
		if (snippetKind === 'mcp') {
			return [
				'command = "roost"',
				'args = ["mcp", "--config", "/path/to/roost.toml"]',
				'env = { ROOST_TOKEN = "roost_••••••••••••" }'
			].join('\n');
		}
		return [
			'curl -H "Authorization: Bearer roost_••••••••••••" \\',
			'  "http://127.0.0.1:8484/v1/collections"'
		].join('\n');
	});

	// Tab 3: 6-tab configurator dashboard preview
	type DashTab = {
		id: 'collections' | 'tokens' | 'status' | 'audit' | 'settings' | 'explore';
		label: string;
		summary: string;
		detail: string;
	};

	const dashTabs: DashTab[] = [
		{
			id: 'collections',
			label: 'Collections',
			summary: 'Add, edit, or remove document folders with the directory picker.',
			detail: 'Choose whether removed documents are pruned from SQLite and clean up orphaned rows.'
		},
		{
			id: 'tokens',
			label: 'Tokens',
			summary: 'Create scoped or expiring tokens and revoke live ones in SQLite.',
			detail: 'Secrets print once on creation; revoking takes effect on the next request.'
		},
		{
			id: 'status',
			label: 'Status',
			summary: 'Check per-collection counts, filter problem files, and inspect full parser errors.',
			detail: 'Trigger a single-collection or full indexing pass straight from the TUI.'
		},
		{
			id: 'audit',
			label: 'Audit',
			summary: 'Filter recent access logs by text or outcome, inspect matched documents, and follow live.',
			detail: 'Resolves logged document IDs back to paths in configured collections.'
		},
		{
			id: 'settings',
			label: 'Settings',
			summary: 'Edit server, storage, and embedding settings with a live TOML preview.',
			detail: 'Saves cleanly to disk and reminds you when a running server needs a restart.'
		},
		{
			id: 'explore',
			label: 'Explore',
			summary: 'Run test searches, inspect extracted sections, and preview token ACL scoping.',
			detail: 'Uses the same core search and reader engine as MCP before connecting a client (or run roost explore).'
		}
	];

	let activeDash = $state<DashTab['id']>('collections');
	const currentDash = $derived(dashTabs.find((t) => t.id === activeDash) ?? dashTabs[0]);
</script>

<div class="toy frame" aria-label="Interactive walkthrough of roost setup">
	<div class="tour-tabs" role="tablist" aria-label="Setup highlights">
		{#each tourTabs as tab (tab.id)}
			<button
				type="button"
				role="tab"
				aria-selected={activeTab === tab.id}
				class:active={activeTab === tab.id}
				onclick={() => (activeTab = tab.id)}
			>
				{tab.label}
			</button>
		{/each}
	</div>

	<div class="panel" aria-live="polite">
		{#if activeTab === 'picker'}
			<div class="phead">
				<span class="tag">Guided 7-step wizard</span>
				<button
					type="button"
					class="mini-btn"
					aria-pressed={showHidden}
					onclick={() => (showHidden = !showHidden)}
				>
					. hidden: {showHidden ? 'on' : 'off'}
				</button>
			</div>

			<p class="explain">
				Browse directories visually, press <code>/</code> to jump straight to a path with <code>Tab</code> completion, and guard against indexing <code>storage.data_dir</code>.
			</p>

			<ul class="folder-list" aria-label="Example folder picker">
				{#each exampleFolders as folder (folder.path)}
					{@const selected = pickedFolders.includes(folder.name)}
					<li>
						<button
							type="button"
							class="folder-row"
							class:selected
							class:blocked={folder.blocked}
							aria-pressed={folder.blocked ? undefined : selected}
							onclick={() => toggleFolder(folder.name, folder.blocked)}
						>
							<span class="mark" aria-hidden="true">
								{#if folder.blocked}
									!
								{:else if selected}
									✓
								{:else}
									+
								{/if}
							</span>
							<span class="fname"><code>{folder.path}</code></span>
							<span class="note">{folder.note}</span>
						</button>
					</li>
				{/each}
			</ul>

			{#if attemptedOverlap}
				<p class="guard-msg" role="status">
					Refused: <code>/srv/roost-data</code> overlaps <code>storage.data_dir</code>, so Roost stops you from indexing its own database.
				</p>
			{:else}
				<p class="status-line">
					Selected collections ({pickedFolders.length}):
					{#if pickedFolders.length === 0}
						<span>none yet — click a folder above</span>
					{:else}
						<code>{pickedFolders.join(', ')}</code>
					{/if}
				</p>
			{/if}
		{:else if activeTab === 'token'}
			<div class="phead">
				<span class="tag">Finish &amp; Token step</span>
				<div class="pill-group" role="group" aria-label="Token scope">
					<button
						type="button"
						class="mini-btn"
						aria-pressed={tokenScope === 'scoped'}
						onclick={() => (tokenScope = 'scoped')}
					>
						scoped
					</button>
					<button
						type="button"
						class="mini-btn"
						aria-pressed={tokenScope === 'all'}
						onclick={() => (tokenScope = 'all')}
					>
						* all
					</button>
				</div>
			</div>

			<p class="explain">
				Review your TOML config, generate your first <code>roost_...</code> secret once (hashed with SHA-256 in SQLite), and copy a ready-to-run client snippet.
			</p>

			<div class="secret-box">
				<div>
					<span class="label">Generated once</span>
					<code>roost_••••••••••••••••</code>
				</div>
				<span class="scope-chip">scope: {scopeLabel}</span>
			</div>

			<div class="snippet-tabs" role="tablist" aria-label="Client snippet">
				<button
					type="button"
					role="tab"
					class="mini-btn"
					aria-selected={snippetKind === 'quack'}
					onclick={() => (snippetKind = 'quack')}
				>
					quack
				</button>
				<button
					type="button"
					role="tab"
					class="mini-btn"
					aria-selected={snippetKind === 'mcp'}
					onclick={() => (snippetKind = 'mcp')}
				>
					MCP stdio
				</button>
				<button
					type="button"
					role="tab"
					class="mini-btn"
					aria-selected={snippetKind === 'rest'}
					onclick={() => (snippetKind = 'rest')}
				>
					REST curl
				</button>
			</div>

			<pre class="snippet"><code>{snippetText}</code></pre>
		{:else if activeTab === 'dashboard'}
			<div class="phead">
				<span class="tag">Opens when roost.toml exists</span>
				<span class="hint"><code>roost setup --wizard</code> reruns onboarding</span>
			</div>

			<div class="dash-tabs" role="tablist" aria-label="Dashboard tabs">
				{#each dashTabs as dt (dt.id)}
					<button
						type="button"
						role="tab"
						class="dash-btn"
						class:active={activeDash === dt.id}
						aria-selected={activeDash === dt.id}
						onclick={() => (activeDash = dt.id)}
					>
						{dt.label}
					</button>
				{/each}
			</div>

			<div class="dash-card">
				<h3>{currentDash.label}</h3>
				<p class="explain">{currentDash.summary}</p>
				<p class="status-line">{currentDash.detail}</p>
			</div>
		{:else}
			<div class="phead">
				<span class="tag">Built for real ops</span>
			</div>

			<ul class="guard-list">
				<li>
					<strong>Atomic <code>0600</code> writes</strong>
					<span>The wizard previews the rendered TOML before saving <code>roost.toml</code> atomically with owner-only permissions.</span>
				</li>
				<li>
					<strong>Outbound embeddings opt-in</strong>
					<span>Off by default. Enabling vector embeddings inside setup shows an explicit outbound-traffic warning first.</span>
				</li>
				<li>
					<strong>Non-TTY refusal + scripted fallback</strong>
					<span><code>roost setup</code> refuses non-interactive pipes; CI and provisioning scripts keep using <code>roost init</code> and CLI flags.</span>
				</li>
			</ul>
		{/if}
	</div>
</div>

<style>
	.toy {
		padding: 16px;
		background: var(--white);
	}

	.tour-tabs {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 8px;
		margin-bottom: 14px;
	}

	.tour-tabs button {
		min-height: 48px;
		padding: 8px 10px;
		border: 2.5px solid var(--ink);
		border-radius: 5px;
		background: var(--paper);
		color: var(--ink);
		font-weight: 800;
		font-size: 0.92rem;
		cursor: pointer;
		text-align: left;
		transition: transform 0.1s ease, background 0.1s ease;
	}

	.tour-tabs button:hover {
		transform: translate(-1px, -1px);
	}

	.tour-tabs button.active {
		background: var(--yellow);
		box-shadow: 3px 3px 0 var(--ink);
	}

	.panel {
		border: 2.5px solid var(--ink);
		border-radius: 5px;
		background: var(--paper);
		padding: 14px;
	}

	.phead {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin-bottom: 10px;
	}

	.tag {
		display: inline-block;
		padding: 3px 8px;
		background: var(--teal);
		border: 2px solid var(--ink);
		border-radius: 3px;
		font-size: 0.78rem;
		font-weight: 800;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.hint {
		font-size: 0.85rem;
		color: var(--ink-soft);
	}

	.explain {
		margin: 0 0 12px;
		font-size: 0.96rem;
		line-height: 1.45;
		color: var(--ink);
	}

	.pill-group,
	.snippet-tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}

	.mini-btn {
		min-height: 44px;
		padding: 6px 12px;
		border: 2px solid var(--ink);
		border-radius: 4px;
		background: var(--white);
		color: var(--ink);
		font-weight: 700;
		font-size: 0.85rem;
		cursor: pointer;
	}

	.mini-btn[aria-pressed='true'],
	.mini-btn[aria-selected='true'] {
		background: var(--yellow);
	}

	.folder-list {
		list-style: none;
		padding: 0;
		margin: 0 0 10px;
		display: grid;
		gap: 6px;
	}

	.folder-row {
		width: 100%;
		min-height: 46px;
		padding: 8px 10px;
		border: 2px solid var(--ink);
		border-radius: 4px;
		background: var(--white);
		color: var(--ink);
		display: grid;
		grid-template-columns: 24px minmax(0, 1fr) auto;
		align-items: center;
		gap: 8px;
		text-align: left;
		cursor: pointer;
	}

	.folder-row.selected {
		background: var(--yellow);
	}

	.folder-row.blocked {
		background: #fff0f6;
		border-style: dashed;
	}

	.mark {
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border: 2px solid var(--ink);
		border-radius: 3px;
		background: var(--white);
		font-weight: 800;
		font-size: 0.85rem;
	}

	.fname {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.note {
		font-size: 0.78rem;
		font-weight: 700;
		color: var(--ink-soft);
	}

	.status-line {
		margin: 0;
		font-size: 0.9rem;
		color: var(--ink-soft);
	}

	.guard-msg {
		margin: 0;
		padding: 8px 10px;
		background: var(--pink);
		border: 2px solid var(--ink);
		border-radius: 4px;
		font-size: 0.88rem;
		font-weight: 700;
		color: var(--ink);
	}

	.secret-box {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 10px 12px;
		background: var(--white);
		border: 2px solid var(--ink);
		border-radius: 4px;
		margin-bottom: 10px;
	}

	.secret-box .label {
		display: block;
		font-size: 0.75rem;
		font-weight: 800;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--ink-soft);
	}

	.scope-chip {
		padding: 4px 8px;
		background: var(--yellow);
		border: 2px solid var(--ink);
		border-radius: 3px;
		font-size: 0.82rem;
		font-weight: 800;
	}

	.snippet-tabs {
		margin-bottom: 8px;
	}

	.snippet {
		margin: 0;
		padding: 10px 12px;
		background: var(--white);
		border: 2px solid var(--ink);
		border-radius: 4px;
		overflow-x: auto;
		font-size: 0.84rem;
		line-height: 1.5;
	}

	.dash-tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-bottom: 10px;
	}

	.dash-btn {
		min-height: 44px;
		padding: 6px 10px;
		border: 2px solid var(--ink);
		border-radius: 4px;
		background: var(--white);
		color: var(--ink);
		font-weight: 700;
		font-size: 0.85rem;
		cursor: pointer;
	}

	.dash-btn.active {
		background: var(--yellow);
		box-shadow: 2px 2px 0 var(--ink);
	}

	.dash-card {
		padding: 12px;
		background: var(--white);
		border: 2px solid var(--ink);
		border-radius: 4px;
	}

	.dash-card h3 {
		margin: 0 0 6px;
		font-size: 1.05rem;
	}

	.guard-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 8px;
	}

	.guard-list li {
		padding: 10px 12px;
		background: var(--white);
		border: 2px solid var(--ink);
		border-radius: 4px;
		display: grid;
		gap: 4px;
	}

	.guard-list span {
		font-size: 0.9rem;
		color: var(--ink-soft);
		line-height: 1.4;
	}

	@media (max-width: 540px) {
		.tour-tabs {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
