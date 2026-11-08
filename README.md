# Roost

Roost gives AI tools search and read access to your internal documents, without
those documents leaving your network.

Point it at a folder of Word, Excel, PowerPoint, PDF, and text files. It keeps an
index up to date and serves it over [MCP](https://modelcontextprotocol.io) and
REST, with per-token access control and an audit log.

- **No outbound calls by default.** No telemetry, update checks, or model
  downloads. This is [tested against the real binary](SECURITY.md#no-outbound-calls).
- **One static binary.** No runtime, database server, or cgo.
- **Built for several users.** Each token sees only the collections it was
  granted, and every access is logged.
- **Citations.** Results name the page, slide, sheet, or heading they came from.
- **Handles hostile files.** Parsers run in isolated, limited child processes.

## Quick start

```sh
make build                             # needs Go 1.26+
cp roost.example.toml roost.toml       # then set your data_dir and collections

./roost token create --name alice --collections legal,hr
./roost serve
```

Save the token: it is shown only once. Then try a search:

```sh
curl -H "Authorization: Bearer $TOKEN" -d '{"query":"acquisition timeline"}' \
  http://127.0.0.1:8484/v1/search
```

## Connecting a client

**MCP over HTTP** suits shared servers. For example, in Claude Code:

```sh
claude mcp add --transport http roost https://roost.internal:8484/mcp \
  --header "Authorization: Bearer $TOKEN"
```

**MCP over stdio** suits a client on the same machine as the index:

```json
{
  "mcpServers": {
    "roost": {
      "command": "/usr/local/bin/roost",
      "args": ["mcp", "--config", "/etc/roost/roost.toml"],
      "env": { "ROOST_TOKEN": "roost_..." }
    }
  }
}
```

**REST** suits RAG pipelines, scripts, and frameworks without MCP. The spec is at
`/openapi.json`, and the same token, access rules, and audit log apply.

### MCP tools

All tools are read-only.

| Tool | Purpose |
|---|---|
| `list_collections` | Collections you can read |
| `search_documents` | Ranked snippets, with filters for collection, type, path, and date |
| `list_documents` | Browse files, including ones whose text is unavailable and why |
| `get_document_info` | Metadata and an outline of sections |
| `read_document` | Text of a whole document or one section, in pages |

### REST API

| Route | Purpose |
|---|---|
| `POST /v1/search` | Search |
| `GET /v1/collections` | List collections |
| `GET /v1/documents` | List documents |
| `GET /v1/documents/{id}` | Metadata and outline |
| `GET /v1/documents/{id}/text` | Read text |
| `GET /v1/documents/{id}/file` | Download the original |

## Supported files

| Format | What is indexed |
|---|---|
| `.docx` `.docm` | Body, tables, footnotes, comments, headers and footers |
| `.xlsx` `.xlsm` | All sheets, including hidden ones (labelled) |
| `.pptx` `.pptm` | Slides, tables, speaker notes |
| `.pdf` | Text by page |
| `.eml` | Headers and body; attachments are listed but not read |
| `.html` `.htm` `.md` `.txt` `.csv` `.tsv` `.rst` `.log` | Text |

Roost never drops a file silently. Run `roost status` to see anything it could
not fully index:

| Status | Meaning |
|---|---|
| `no_text` | No extractable text, typically a scanned PDF. There is no OCR yet |
| `unsupported` | A format Roost cannot read, such as legacy `.doc`, `.xls`, or `.ppt` |
| `encrypted` | Password protected |
| `too_large` | Over `index.max_file_mb` |
| `failed` | Corrupt, or the parser crashed or timed out |
| `(truncated)` | More text than `index.max_text_mb`; the start was kept |

## Search

Search uses BM25 with stemming, so "acquisition" finds "acquisitions". Use
`"double quotes"` for a phrase and `term*` for a prefix. If no passage contains
every word, Roost falls back to matching any of them, and says so in `mode`.

Keyword search misses a document that says the same thing in other words. To
cover that, you can [enable semantic search](roost.example.toml) through an
embeddings service on your own network, such as Ollama, vLLM, or TEI. Results
from both methods are merged. If the service goes down, search carries on with
keywords and reports it in `warning`.

Enabling this sends document text to that service. See
[SECURITY.md](SECURITY.md#if-you-enable-embeddings).

## Commands

```
roost serve                    Index documents and serve MCP and REST
roost mcp                      Serve MCP over stdio (needs ROOST_TOKEN)
roost index [--prune]          Run one indexing pass, then exit
roost status                   Show coverage and problem files
roost audit [-n N]             Show recent access log entries
roost token create             --name NAME --collections a,b|--all [--ttl 720h]
roost token list
roost token revoke ID
```

Flags go before positional arguments, as in `roost token revoke --config x.toml ID`.

## Deploying

- **Binaries:** `make release` cross-compiles for Linux (amd64 and arm64);
  pushing a `v*` git tag publishes them to GitHub Releases after CI runs
  `make test`, `make race`, and `make e2e` against the tagged commit.
- **systemd:** `deploy/roost.service` is hardened and blocks outbound traffic at
  the OS level.
- **Container:** see the `Dockerfile`. The image holds only the binary.
- **Air-gapped builds:** `make offline-bundle` packages the source with every
  dependency.

Rescans are the source of truth, so files on NFS and SMB shares are picked up even
though file watching does not work there. If a share unmounts, Roost keeps the
index rather than treating every file as deleted. If a collection really is empty
now, confirm with `roost index --prune`.

## Performance

Measured on a 6-core i5-9400F, with a synthetic corpus of 50,000 text files
(391 MB, 300,000 chunks):

| Measure | Result |
|---|---|
| Full index | 1m58s, 46 MB peak for the server process |
| Index size | 652 MB, about 1.7× the text |
| Search, rare term | 21 ms median |
| Search, two terms | 21 ms median, 146 ms p95 |
| Search, six-word question | 320 ms median, 880 ms p95 |
| Search, a term in nearly every chunk | 710 ms median, 1 s p95 |
| Vector scan, 200,000 × 768 dimensions | 30 ms, excluding the embedding call |

Latency grows with the number of matching chunks, so expect worst-case queries to
slow down in proportion on larger collections. Real Office and PDF files take
longer to index than plain text. Semantic search holds vectors in memory, at about
3 KB per chunk for 768 dimensions.

## Limitations

- No OCR, so scanned PDFs are not searchable.
- No legacy `.doc`, `.xls`, or `.ppt`. Convert them first.
- Email attachments are not extracted.
- Access control is per collection and does not read file system permissions.
- Single node: no clustering or high availability.
- No SSO yet: authentication is by API token.
- PDF extraction was compared with `pdftotext` on 12 technical manuals: median
  99.7% of its vocabulary recovered, worst case 97.5%. Heavily designed layouts,
  right-to-left text, and vertical text have not been evaluated.

## Developing

```sh
make test    # unit tests
make race    # with the race detector
make e2e     # real binary under strace; needs strace, curl, openssl, python3
make fuzz    # fuzz the extractors
```

`scripts/gen-fixtures.sh` regenerates test documents with LibreOffice.

## License

[Apache-2.0](LICENSE)
