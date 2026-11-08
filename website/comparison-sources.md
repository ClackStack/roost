# Where the comparison table's facts come from

Checked against each project's README (ctxd: the docs at ctxd.dev/docs, which
its README names as the source of truth). alcove checked 2026-11-04, ctxd
2026-11-06. Recheck before editing the table, and update the date on the page
when you do. "Not listed" means the docs don't mention it.

## ctxd (ctxd-dev/ctxd)

| Cell | Source |
|---|---|
| No, text files only | Local tracker, "What gets tracked": the indexable extension list (.cfg .css .csv .html .ini .json .log .md .py .rst .toml .ts .tsx .txt .yaml .yml), valid UTF-8 only |
| No, binaries are excluded | Local tracker: "ctxfs does not expose arbitrary binary files, extensionless files, files with unsupported extensions" |
| Scoped tokens, not multi-tenant | Security: "a single ctxfs namespace with scoped credentials, not a multi-tenant security boundary" |
| Yes, HTTP and a stdio bridge | ctxfs API: "an MCP endpoint at `POST /mcp`"; ctxfs, "MCP": the `ctxd-mcp` stdio bridge |
| Python 3.11 or 3.12, plus ripgrep | Local tracker, "Requirements": "Python 3.11 or 3.12 with `pip`"; ctxfs: "ripgrep is a required runtime dependency (version 13.0.0 or newer)" |
| No, by design | Overview: "ctxfs is not a semantic-search index"; Security, "What ctxd does not store": "A semantic-search embedding index" |
| None mentioned; local by default | Quickstart: "The default Unix-socket workflow does not require an API key"; no telemetry is mentioned anywhere |
| Some: .py, .ts and .tsx only | The extension list above; other code extensions (.js, .go, .rs, ...) are not in it |
| Audit log, pulls from URLs | Not mentioned in the README or docs |

## alcove (epicsagas/alcove)

| Cell | README |
|---|---|
| PDF, best with pdftotext | Optional tools table lists `pdftotext`; without it alcove falls back to a built-in parser that may fail on some files |
| Optional model download | "During `alcove setup`, you can choose an embedding model and download it immediately" |
| Multi-user on its roadmap | Roadmap: "Multi-user remote access" |
| HTTP API is its main route | Intro: "an HTTP API server"; `/mcp` is labelled "legacy", MCP "still available" |
| A Rust binary | Published on crates.io, installed with cargo or an installer script |
| Semantic search, about 90 MB model | "The default model is ArcticEmbedXS (90 MB, multilingual)"; `alcove model download` |
| Code indexing with tree-sitter | Intro: "tree-sitter code indexing" |
