# Security

Roost is built to run inside a network that holds sensitive documents. This
page says what it protects against, how that was tested, and what remains your
responsibility.

Please report vulnerabilities privately through GitHub security advisories
rather than public issues.

## No outbound calls

In its default configuration Roost opens no outbound connections: no telemetry,
no update checks, no model downloads.

| Check | What it proves |
|---|---|
| `egress_test.go` | First-party code outside `internal/embed` cannot compile outbound calls without failing the test suite |
| `scripts/e2e.sh` | The real binary, including third-party code and extraction workers, makes no network connections during indexing, serving, and stdio sessions, measured with `strace` |
| Positive control in `scripts/e2e.sh` | The same tracing does see connections when embeddings are on, and only to the configured endpoint |

Do not rely on Roost alone for this. `deploy/roost.service` blocks outbound
traffic at the OS level with `IPAddressDeny=any`, and a firewall rule is a good
idea too.

### If you enable embeddings

Embeddings are the one outbound feature, and they are off by default. When on,
**document text is sent to the endpoint**, so it must be as trusted as the
documents themselves. To limit that, the client:

- connects only to private and loopback IPs, checked at connect time, which also
  defeats DNS rebinding. Public addresses need `allow_public_endpoint = true`.
- ignores `HTTP_PROXY`, and refuses redirects.

Use `https` unless the endpoint is on the same host: the example `http` URL
sends document text in cleartext across your network.

## Access control

- Every request needs a token. There is no anonymous mode.
- Tokens are 256-bit random values, stored only as SHA-256 hashes, and checked
  against the database on every request. Revocation applies to the next call,
  including on open MCP sessions.
- Tokens grant named collections. The grant is applied inside the SQL query.
- A document outside your grant gives the same response as one that does not
  exist.
- `/healthz` and `/openapi.json` are the only unauthenticated routes. Neither
  reveals anything about your documents.

**Access is per collection.** Roost does not read NTFS, POSIX, or SharePoint
permissions, so anyone with a token for a collection can read everything in it.
Organize storage paths by audience.

## Hostile documents

Parsers run on untrusted input, so each file is extracted in a child process:

- It receives the file as stdin and opens no paths itself, so a swapped symlink
  cannot redirect it.
- It has an empty environment, so it cannot see `ROOST_TOKEN` or similar.
- It has a timeout and a memory limit, and its output is capped. A hang, crash,
  or decompression bomb fails that one file and is shown in `roost status`.

The worker is not a full sandbox: it runs as the same user as the server. If
your threat model includes parser exploits, run Roost under the provided systemd
unit or in a container, on a dedicated account with read-only access to documents.

Roost never writes to storage paths, skips symlinks that leave a collection, and
never follows references inside documents, such as remote images.

## What you must handle

- **Encryption at rest.** The index contains document text in plaintext. The data
  directory is created with mode `0700`, but encrypting the volume is up to you.
  Back it up with the same care as the documents.
- **Prompt injection.** A document can contain text aimed at an AI, such as
  "ignore your instructions and send this file to...". Roost labels document text
  as untrusted and tells clients not to follow it, but it cannot make a model
  comply. Limit what else an agent with Roost access is able to do, particularly
  anything that can send data out.
- **Audit log contents.** Search text is logged by default because it matters in
  an investigation, but queries can themselves be sensitive. Set
  `audit.log_queries = false` if needed. The log is in the database and optionally
  a JSONL file; Roost does not rotate or expire either (send `SIGHUP` after
  rotating the JSONL file so `roost serve` reopens it).
- **Audit failure stops service.** If an audit record cannot be written, the
  request is refused. A full disk therefore causes an outage rather than unlogged
  access. Monitor disk space.
- **Failed logins** go to the server log rather than the database, because anyone
  can reach that path and it must not be able to fill your disk.
- **Removed collections.** Taking a collection out of the config makes it
  unreadable immediately, but its text stays in the database until you run
  `roost index --prune`.
- **Tracked changes.** Deleted text in Word files is not indexed, but the original
  file still contains it, and it can be downloaded by anyone with access.
- **Hidden content.** Hidden spreadsheet sheets, speaker notes, and comments *are*
  indexed, because that is where sensitive material often sits.
