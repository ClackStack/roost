# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Marketing site lives in `website/`: SvelteKit 3 with static renderer output (user's choice). The product itself is Go.

## Users

Primary: platform and infrastructure engineers at organisations with sensitive internal documents. They find Roost, try the binary, and argue for it internally. They read specs, distrust marketing, and check claims themselves. A security reviewer usually has to approve what they pick, so claims must survive that review too.

## Product Purpose

Roost gives AI tools search and read access to internal documents without those documents leaving the network. Point it at a folder of Word, Excel, PowerPoint, PDF and text files; it keeps an index current and serves it over MCP and REST. Success is an engineer running it inside their network and connecting an AI client the same day.

## Positioning

A single static Go binary that makes no outbound calls by default, verified by tracing the real binary, not just promised. Unlike local-search-mcp (Node, text and code only, built around fetching from the internet) and alcove (Rust, Markdown/PDF/code, single-user, downloads models at setup), Roost reads Office files and has multi-user access control.

## Operating Context

Self-hosted inside an enterprise network, including air-gapped ones. Documents live on local disks or NFS/SMB shares. Consumers are MCP clients over HTTP or stdio, and RAG pipelines or scripts over REST. Operators work through a CLI, a TOML config, systemd or a container.

## Capabilities and Constraints

- No telemetry, update checks or model downloads. The only outbound code is opt-in embeddings to a service the operator configures, private addresses by default.
- One static binary, about 16 MiB, no cgo, runtime or database server. SQLite FTS5 inside.
- Formats: docx, xlsx, pptx, pdf, eml, html, md, txt, csv, tsv, rst, log. Results cite page, slide, sheet or heading.
- Five read-only MCP tools; REST with OpenAPI; same token, access rules and audit log on both.
- Per-token collection grants enforced in the SQL query; audit failure refuses the request.
- Parsers run in isolated, limited child processes.
- Not in v1: OCR, legacy .doc/.xls/.ppt, email attachments, SSO, clustering, per-document ACLs, encryption at rest.
- Licence: Apache-2.0.
- Repo: https://github.com/ClackStack/roost (release binaries for Linux amd64/arm64).

## Brand Commitments

Name: Roost. The user wants the site fun and playful, focused on technical differences from other options, and easy to read. The marketing site itself makes zero third-party requests: self-hosted fonts, no analytics, trackers or CDNs. The primary action on the site is downloading a binary.

## Evidence on Hand

From `README.md` and `SECURITY.md`, measured on a 6-core i5-9400F with a synthetic 50,000-file text corpus (391 MB, 300,000 chunks): full index 1m58s, 46 MB peak; search 21 ms median for a rare term, 320 ms median for a six-word question, 710 ms median for a term in nearly every chunk; 200,000 × 768 vector scan 30 ms. Under strace: zero network connections for serve and stdio. 52 of 52 end-to-end checks. All 17 deliberately broken security controls caught by tests. PDF vocabulary versus pdftotext on 12 manuals: 99.7% median, 97.5% worst.

Competitor facts on the site are sourced line by line in `website/comparison-sources.md`.

Absent, must not be fabricated: customers, testimonials, logos, star counts, download counts, pricing, third-party audits, certifications. Not verified: real MCP client apps, Docker image, Windows and macOS runtime, real NFS/SMB shares.

## Product Principles

- Prove it rather than promise it: every technical claim points at something a reader can rerun.
- Publish the limits alongside the strengths. The slowest search number ships next to the fastest.
- Nothing leaves unless the operator explicitly sends it.
- Boring to operate: one file, one config, no surprises.
