<script lang="ts">
	type Doc = { file: string; where: string; text: string };

	// Invented documents. Nothing here comes from a real organisation.
	const pile: Doc[] = [
		{ file: 'Project Heron board deck.pptx', where: 'slide 7', text: 'Acquisition timeline: due diligence closes in March and signing is targeted for April.' },
		{ file: 'M&A checklist.xlsx', where: 'sheet Legal', text: 'Acquisitions over five million need board approval before the term sheet.' },
		{ file: 'Parental leave policy.docx', where: 'heading Eligibility', text: 'Parental leave is available after six months of service, for any new parent.' },
		{ file: 'FY26 budget.xlsx', where: 'sheet Headcount', text: 'Travel budget is set per team and budgeted against third quarter actuals.' },
		{ file: 'Vendor security review.pdf', where: 'page 12', text: 'The vendor encrypts backups at rest and rotates keys every ninety days.' },
		{ file: 'Office move FAQ.docx', where: 'heading Moving day', text: 'Desks will be moved over the weekend, so pack your things by Friday.' },
		{ file: 'Hiring update.eml', where: 'message body', text: 'We are hiring two platform engineers and one security reviewer this quarter.' },
		{ file: 'Incident review.pdf', where: 'page 3', text: 'The outage began when the primary database failed over during a backup.' }
	];

	const examples = ['acquisition', '"parental leave"', 'back*', 'moving desks'];

	const stem = (w: string) => w.toLowerCase().replace(/(ing|ed|es|s)$/, '').replace(/e$/, '');
	const words = (s: string) => s.match(/[\p{L}\p{N}]+/gu) ?? [];

	type Term = { test: (word: string) => boolean; phrase?: string };

	function parse(q: string): Term[] {
		const terms: Term[] = [];
		const rest = q.replace(/"([^"]+)"/g, (_, p: string) => {
			const parts = words(p).map(stem);
			terms.push({ test: (w) => parts.includes(stem(w)), phrase: words(p).join(' ').toLowerCase() });
			return ' ';
		});
		for (const raw of rest.split(/\s+/).filter(Boolean)) {
			const prefix = raw.endsWith('*');
			const w = words(raw)[0];
			if (!w) continue;
			const s = stem(w);
			terms.push({
				test: prefix ? (x) => x.toLowerCase().startsWith(w.toLowerCase()) : (x) => stem(x) === s
			});
		}
		return terms;
	}

	const hit = (d: Doc, t: Term) =>
		t.phrase ? words(d.text).join(' ').toLowerCase().includes(t.phrase) : words(d.text).some(t.test);

	function segments(text: string, terms: Term[]) {
		return text.split(/([\p{L}\p{N}]+)/u).map((s, i) => ({ s, i, on: terms.some((t) => t.test(s)) }));
	}

	let query = $state('acquisition');
	let terms = $derived(parse(query));
	let all = $derived(pile.filter((d) => terms.length > 0 && terms.every((t) => hit(d, t))));
	let any = $derived(pile.filter((d) => terms.some((t) => hit(d, t))));
	let widened = $derived(all.length === 0 && any.length > 0);
	let results = $derived(all.length ? all : any);
	let status = $derived(
		terms.length === 0
			? 'Type something to search the pile.'
			: results.length === 0
				? 'Nothing in this little pile. Try one of the examples.'
				: widened
					? `No passage had every word, so here are ${results.length} that match some of them.`
					: `${results.length} ${results.length === 1 ? 'passage' : 'passages'} found.`
	);
</script>

<div class="toy frame">
	<label for="toy-q">Search a pretend pile of documents</label>
	<div class="frame field"><input id="toy-q" type="search" bind:value={query} autocomplete="off" spellcheck="false" /></div>
	<div class="ex">
		{#each examples as e (e)}
			<button type="button" class={[query === e && 'on']} onclick={() => (query = e)}>{e}</button>
		{/each}
	</div>
	<p class="status" aria-live="polite">{status}</p>
	<ul>
		{#each results as d (d.file)}
			<li>
				<strong>{d.file}</strong> <span class="where">{d.where}</span>
				<p>
					{#each segments(d.text, terms) as g (g.i)}{#if g.on}<mark>{g.s}</mark>{:else}{g.s}{/if}{/each}
				</p>
			</li>
		{/each}
	</ul>
	<p class="note">
		All invented, and this toy runs in your browser. The real thing is BM25 with stemming, phrases and prefixes, and it names the page, slide, sheet or heading too.
	</p>
</div>

<style>
	.toy {
		background: var(--white);
		padding: clamp(16px, 2.4vw, 28px);
		min-width: 0;
	}
	label {
		display: block;
		font: 400 1.5rem/1.15 var(--marker);
		margin-bottom: 10px;
	}
	.field {
		background: var(--paper);
	}
	input {
		width: 100%;
		min-height: 52px;
		padding: 0 16px;
		font: 700 1.15rem var(--body);
		color: inherit;
		caret-color: var(--pink);
		background: none;
		border: 0;
		border-radius: 10px;
	}
	.ex {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 10px;
	}
	.ex button {
		font: 400 0.9rem var(--code);
		min-height: 44px;
		padding: 0 12px;
		background: none;
		color: inherit;
		border: 2.5px solid var(--ink);
		border-radius: 10px 7px 12px 6px;
		cursor: pointer;
	}
	.ex button:hover,
	.ex button.on {
		background: var(--yellow);
	}
	.status {
		font-weight: 700;
		margin: 14px 0 8px;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		height: 300px;
		overflow-y: auto;
		scrollbar-width: thin;
	}
	li {
		padding: 10px 0;
		border-top: 2px dashed var(--ink);
	}
	li p {
		margin: 4px 0 0;
	}
	.where {
		display: inline-block;
		font-size: 0.9em;
		background: var(--teal);
		padding: 0 8px;
		border-radius: 6px;
		font-weight: 700;
	}
	mark {
		background: var(--yellow);
		color: inherit;
	}
	.note {
		margin: 12px 0 0;
		color: var(--ink-soft);
		font-size: 0.95em;
	}
</style>
