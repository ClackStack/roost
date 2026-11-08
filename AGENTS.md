# AGENTS.md

Roost is a self-hosted document search server: it indexes Office/PDF/text files
and serves them to AI tools over MCP and REST, with per-token access control and
an audit log. Two codebases in one repo:

- **Go server** (repo root): `cmd/roost` + `internal/*`. Go 1.26+, no cgo,
  one static binary.
- **Marketing site** (`website/`): SvelteKit 3 static site, Svelte 5 runes,
  no runtime deps.

The product's core promise is **no outbound network calls by default**. This is
an enforced invariant, not a convention — see "Egress rule" below. Security
posture dominates many design decisions; read SECURITY.md before changing
anything security-adjacent.

## Commands

```sh
make build     # CGO_ENABLED=0 static build -> ./roost
make test      # go vet + go test ./...
make race      # tests with race detector
make e2e       # real binary under strace; needs strace, curl, openssl, python3
make fuzz      # 5m fuzz of internal/extract (FuzzExtract)
make release   # cross-compile linux/darwin/windows into dist/
make clean
```

Website (run inside `website/`): `npm run dev`, `npm run build` (static output
to `build/`), `npm run preview`, `npm run check` (svelte-check; the only
typecheck). `.npmrc` sets `engine-strict=true`. Both `package-lock.json` and
`pnpm-lock.yaml` exist.

Running locally: `make build && cp roost.example.toml roost.toml` (edit
`data_dir` + collections), `./roost token create --name x --collections a,b`
(secret prints exactly once), `./roost serve`. CLI gotcha: flags go *before*
positional args (`roost token revoke --config x.toml ID`).

`scripts/gen-fixtures.sh` regenerates `internal/extract/testdata/` using
LibreOffice + reportlab; fixtures are committed so tests don't need those tools.
Don't hand-edit them.

## CI/CD (GitHub Actions)

- `ci.yml` runs on every push to `main` and every PR, Linux only:
  `make test` + `make race` in one job, `make e2e` (installs strace) in
  another.
- `release.yml` runs on `v*` tags: the same two gate jobs, then
  `make release offline-bundle VERSION=<tag>` and a GitHub Release holding
  `dist/*` (amd64+arm64 binaries, SHA256SUMS, offline source bundle). A tag
  can never publish untested code.
- Linux-only is deliberate; the platform list is `PLATFORMS` in the Makefile
  and stays the single source of truth for both local and CI releases.
- `install.sh` (repo root) is the `curl | sh` installer for release binaries.
  The website build emits it at `https://roost.clackstack.dev/install.sh`
  (`website/vite.config.ts`), and that is the URL the README gives. It
  depends on release artifacts keeping their names (`roost-<tag>-linux-<arch>`
  plus `SHA256SUMS`) and on `roost version` existing. Before changing its URL
  building or artifact logic, test it against a fake release served by
  `python3 -m http.server` (sed the base URL in a copy; exercise happy path,
  corrupt checksum, and missing checksum).

## Testing the MCP interface with quack

quack, the author's other project, is a terminal coding agent (a separate
repo; its own AGENTS.md covers building and running it). It is a real MCP
client and the natural way to exercise roost's MCP surface by hand, beyond
the raw JSON-RPC that `scripts/e2e.sh` sends.

Register roost as an MCP server in quack's user layer,
`~/.config/quack/mcp.d/roost.toml`:

```toml
# Remote: MCP over HTTP, same endpoint and bearer token as any other client.
url = "http://127.0.0.1:8484/mcp"
headers = { Authorization = { env = "ROOST_TOKEN", format = "Bearer {}" } }
[tools]
auto_approve = ["list_*", "search_*", "get_*", "read_*"]
```

```toml
# Local: MCP over stdio, runs the roost binary itself.
command = "/path/to/roost"
args = ["mcp", "--config", "/path/to/roost.toml"]
env = { ROOST_TOKEN = "roost_..." }
```

- quack's defaults bundle ships a ready-made template for roost itself:
  `defaults/mcp.d/roost.toml` in the quack repo (disabled by default, like all
  bundled examples; the parse/validate test there is
  `shipped_examples_parse` in `crates/quack-config/src/mcp.rs`). Keep the two
  copies in sync.
- The token comes from an environment variable, not the config file.
- If a `[policy.mcp]` allowlist is configured (`allowed` id globs, `hosts` for
  remote servers), the roost entry must match it or quack refuses it.
- A quack run from its checkout needs `QUACK_DEFAULTS_DIR=defaults` — its
  defaults bundle is not compiled into the binary.

## Go architecture

Data flow:

- **Write path**: `indexer` walks collections (periodic rescan is the source of
  truth; fsnotify is only a speed-up) → `extract.Runner` parses each file in a
  child process → `chunk.Split` cuts section-aligned chunks → `store.ReplaceDocument`
  swaps doc+chunks in one SQLite transaction. After each pass, `embed.Pipeline`
  (optional) embeds pending chunks.
- **Read path**: REST handler or MCP tool → `auth.Verify` (hits the DB on every
  request so revocation is immediate) → `core.Service` → `store` (FTS5 BM25,
  optionally fused with vector results via reciprocal-rank fusion) → audit.

Key rule: **`internal/core` is the single implementation of every client
operation.** `internal/rest` and `internal/mcpserver` are thin adapters over
`core.Service`. New features go in core; never duplicate logic in an adapter.

Packages: `config` (strict TOML), `auth` (SHA-256-hashed tokens), `extract`
(hostile-file parsers), `chunk`, `indexer`, `store` (modernc SQLite + FTS5),
`embed` (the ONLY networked package), `core`, `rest`, `mcpserver`.

### Egress rule (hard constraint)

`egress_test.go` at the repo root parses the AST and **fails the build** if any
non-test file outside `internal/embed` references `http.Client/Get/Post/...`,
`net.Dial*`, `tls.Dial`, DNS lookups, or `exec.LookPath`. `scripts/e2e.sh`
additionally strace-traces the real binary (including third-party deps and
workers) to prove zero connections, with a positive control (embeddings on must
show connections). If you add outbound code outside `internal/embed`, tests
fail by design.

Also no cgo anywhere: SQLite is `modernc.org/sqlite` (pure Go), builds pin
`CGO_ENABLED=0`.

### Extraction worker model

The binary re-execs itself as the sandbox child: `extract.WorkerCommand`
(`"extract-worker"`) is checked **first** in `main()` before any config, DB, or
logging. The child receives the file via stdin (never a path), an empty
environment, an rlimit memory cap (`limit_unix.go` / no-op `limit_other.go`
build tags), a timeout, and a capped-size JSON reply. `TestMain` in
`internal/extract` and `internal/indexer` tests re-exec the *test binary* the
same way, so unit tests exercise the real child-process path. Under `-race`
the child's runtime alone reserves ~2 GB of address space before any parsing,
so tests size the sandbox with `testWorkerMemoryMB` (build-tagged files)
instead of the production default.

`extract.Version` is stored per document; bump it to force a full re-index when
extraction output changes.

### Store details

- Two `sql.DB` handles: writer with `MaxOpenConns(1)` + `_txlock=immediate`,
  separate reader pool. WAL, `busy_timeout`, `synchronous(NORMAL)`.
- Migrations are a single `[]string` in `internal/store/schema.go`, applied in
  order via `PRAGMA user_version`. To change schema: append a statement. A
  newer schema than the binary refuses to open.
- Access control is applied *inside SQL*: every read method takes the allowed
  collections. `GetDocument` returns the same `ErrNotFound` for "missing" and
  "not yours" to prevent ID probing.
- FTS5 is external-content (`content='chunks'`); the `CROSS JOIN` in
  `SearchChunks` is deliberate (bm25()/snippet() need the FTS table as outer
  loop). User text never reaches `MATCH` raw — `core/match.go` quotes every
  term (only `"phrases"` and trailing `*` are honored).

### Other security-shaped gotchas

- Config decoding is strict: unknown TOML keys are hard errors (a typo can't
  silently disable a security setting). Keep it that way.
- Audit is fail-closed: if the audit write fails, the client gets an error, not
  results. It uses `context.WithoutCancel` so a client disconnect can't cancel
  its own audit record.
- Logs go to **stderr only** — stdout carries the MCP stdio protocol.
- Tokens: 256-bit random, stored as plain SHA-256 (no KDF, deliberately);
  removing a collection from config instantly revokes access even though rows
  remain (grants intersect with *currently configured* collections).
- Chunking invariants: chunks never span sections (every hit cites one
  page/slide/sheet) and never overlap (joining chunks reproduces the document).
- Spreadsheets: excelize size limits are set specifically so it never spills
  sheets to plaintext temp files that would outlive a killed worker.
- An unmounted NFS/SMB share keeps its index; deletes only happen after a
  complete walk. `roost index --prune` is the explicit override.
- e2e.sh is the proving ground for public claims in README/SECURITY.md; if you
  change behavior it asserts, update both together.

## Go conventions

- Standard library only in tests (no testify). Table-driven `cases := []struct`
  loops; per-package unexported `setup(t)` returning a fixture/env struct with
  `t.Helper()` methods (`e.add`, `f.write`, `e.do`).
- Errors: `fmt.Errorf("context: %w", err)`; sentinel errors (`ErrNotFound`,
  `ErrInvalid`, ...) checked with `errors.Is`; `errors.Join` for multi-failures.
- `*slog.Logger` is injected, never global. `context.Context` is the first
  parameter everywhere.
- Modern stdlib idioms: `wg.Go(...)`, `for range n`, `sync.Map`,
  `context.WithoutCancel`.
- Test names describe behavior ("TestAuditFailureWithholdsResults"). Security
  invariants get dedicated tests (permissions, symlink escapes, ACL matrix,
  revocation on live sessions) — preserve them when touching those paths.
- Commit messages: plain imperative summaries, no conventional-commit prefixes
  (see `git log`).

## Website conventions

- Pushing to `main` with website changes triggers `.github/workflows/website.yml`
  (GitHub Actions): pnpm install/check/build, then `bunny sites deploy build
  --site roost` to the Bunny CDN. It needs the repo secret `BUNNYNET_API_KEY`
  (account-wide); CI installs with pnpm, so keep
  `pnpm-lock.yaml` current (the npm lockfile is not what CI uses).
- **No `svelte.config.js`**: all SvelteKit config (adapter-static, CSP, compiler
  options) lives in `website/vite.config.ts`.
- Svelte 5 runes mode is forced project-wide in that config; use `$props`,
  `$state`, `$derived`, `{@render children()}`, `onclick` attributes. No `$:`,
  no `on:`, no `createEventDispatcher`.
- **Zero third-party requests is a hard product promise**, enforced by the CSP
  in vite.config.ts (`default-src 'none'`; `assetsInlineLimit: 0` because CSP
  refuses `data:` URLs). Fonts are self-hosted via `@fontsource/*` imports. No
  analytics, CDNs, or Google Fonts, ever.
- Import alias `#lib/*` → `src/lib/*` (package.json `imports` field), used from
  routes; relative imports within lib.
- `src/lib/site.ts` centralizes everything release-coupled: version, repo URL,
  download/checksum URLs, platform list. Update it when releases change.
- Visual design: hand-drawn zine aesthetic; tokens and type scale are specified
  in `DESIGN.md` (root), implemented as CSS custom properties in
  `src/app.css`. The `feTurbulence`/`feDisplacementMap` filter `#wob` (defined
  in `+layout.svelte`) is what makes borders look hand-drawn.
- Competitor claims on the site must stay sourced line-by-line in
  `website/comparison-sources.md`; `PRODUCT.md` lists what may NOT be claimed
  (no customers, testimonials, download counts, etc.). Don't fabricate evidence.
- Animations are CSS-only, scroll-driven (`animation-timeline: view()`) gated
  behind `@supports` + `prefers-reduced-motion`.

## Docs map

- `README.md` — user-facing; commands, formats, performance numbers.
- `SECURITY.md` — threat model and how each claim is tested.
- `PRODUCT.md` — product/positioning facts for the site (including the
  do-not-fabricate list).
- `DESIGN.md` — website design tokens.
- `deploy/roost.service` — hardened systemd unit (OS-level egress block);
  `Dockerfile` — scratch image containing only the binary.
