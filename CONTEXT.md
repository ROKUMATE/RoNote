# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P08, import pipeline and API (backend only): done.
- A user POSTs a URL; the worker fetches it, or reuses the cached copy, extracts it and stores the user's document; the API reports progress over SSE and serves documents.
- Demo on the dev stack: importing go.dev's Effective Go streams `queued → fetching → extracting → done`, the document has its 153 code blocks, and a second POST returns the same document in milliseconds.
- `make test-live` still passes all 11 pages.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (ADR 0001).
- Source is verbatim and code byte-identical (ADR 0002), enforced by `TestCodeIsVerbatim`.
- Commits are one line, `Type: Summary`; the hook (strict mode in CI) enforces it (ADR 0003).
- PRs merge by rebase only, so main keeps one-line commits.
- CI runs on the pinned `ubuntu-26.04` image. `.github/actionlint.yaml` declares that label until actionlint knows it (rhysd/actionlint#682).
- License: MIT. Agents never push. Run `make check` unpiped and read its exit code.

IR (`internal/ir`, ADR 0004):
- **Blocks:** a closed set of heading, paragraph, list, code, table, callout, image, quote, divider and tabs.
  - List and tab items are `{label, children}`; only tabs have a label.
  - Code has `lang`, an optional `title` (a file name or frame label) and an optional `title_link` (`{href, kind}`) when the title links to that file.
- **Frozen at `ir_version` 1** since P08: documents are stored as IR, so any shape change needs a version bump and an upgrade path for stored documents.
- **Inline runs** carry marks and links (anchor, same_domain or external).
- **`Validate`** checks every rule.
- **Block IDs:** sha256 of heading path, kind and text (code includes its language and title), with `-n` for repeats.
- **Warnings:**
  - `low_confidence`: no main element.
  - `parse_failed`: a page the parser rejects.
  - `adapter_fallback`: a page an adapter recognised that lacked its structure, extracted with the generic rules instead.

Verbatim rule (refined in P07):
- Code text is the page's copy payload when it has one. Expressive Code's `data-code` uses U+007F for each line break. Without it, Expressive Code's lines would run together, since each line is a separate element with no newline.
- Otherwise it's the `<pre>` text, minus what is never text:
  - line-number gutters;
  - unselectable prompts;
  - Pygments `.linenos`;
  - copy labels and frame chrome;
  - mdBook's boring lines, which its copy button leaves out.
- Visible text stays: `>>>` prompts, diff lines. Pattern rewrites done by copy buttons are not followed.
- The one "what the reader sees" exception: FastAPI's terminal blocks (`div.termy`) drop the `<font>`/`<span>` tags they hold as text, with language `console`.
- `TestCodeIsVerbatim`'s token walker applies the same rules independently, with per-adapter rules keyed by the chosen adapter.

Fetcher (`internal/fetch`):
- **Rules:** http(s) only, ports 80 and 443 only, a 5 MB cap, `text/html` or XHTML only, charset decoded to UTF-8.
- **Timeouts:** 5s connect, 20s total.
- **User agent:** `RoNoteBot/0.1`.
- **SSRF guard:** runs at dial time, on redirects too (at most 5).
  - Always blocked: unspecified, multicast, reserved, IPv4-mapped IPv6 and 169.254.169.254.
  - Also blocked: loopback, private, link-local, CGNAT, ULA and NAT64, unless `AllowPrivateNetworks`. That option also lifts the port rule, and `RONOTE_FETCH_ALLOW_PRIVATE` may set it only in dev and test.
- **robots.txt:** RFC 9309 subset, cached per origin for 1h. 404/410 allow everything; other 4xx deny. A 5xx is `ErrRobotsUnavailable`: nothing is fetched, the answer is not cached, and imports retry it. All 11 live pages allow RoNoteBot.
- **Rate limit:** 1 request per second per host.
- **Conditional GET:** sends ETag and Last-Modified.
- **Status errors** carry the server's `Retry-After`.

Extractor (`internal/extract`):
- **Generic rules, version 2:**
  - Root: main, then article, then `[role=main]`, else body without header and footer, plus `low_confidence`.
  - A fixed removal list.
  - Callouts from class or role markers.
  - Heading anchors.
  - Expressive Code frames: payload, title and language.
  - Linear time on adversarial input.
- **Adapters (`internal/extract/adapter`):** configuration only, with the extractor carrying out each adapter's plan.
  - Registry order: Starlight, Docusaurus, Material (MkDocs Material and Zensical), Sphinx, VitePress, mdBook, Docsy, go.dev, MDN, DocBook.
  - Detection: generator, then DOM signature, then host. go.dev, MDN and kubernetes.io are found by host.
  - Fallback guard: an adapter may drop content only through its Remove list. A root that misses a heading or code block the generic rules keep, or removed chrome holding code, falls back to generic.
  - PostgreSQL gets a DocBook adapter, since generic picked up its site banner, version lists and breadcrumb.
- **Fixtures:** 42 golden fixtures in `testdata/golden`.
  - At least two per adapter, plus `adapter-fallback` and `detect-generator-over-host`.
  - Real excerpts are used only where the license allows, recorded in `SOURCES.md`. MDN, myengineeringpath.dev and docsy.dev are synthetic.
- **Junk checks:** `TestGoldenHasNoJunk` checks every fixture for chrome, and `make test-live` checks the live pages with the same checker.
- **Tests that catch regressions:** three deliberate breaks were each caught.
  - A copy label leaking into Starlight code.
  - Material keeping line numbers.
  - Detection preferring host over generator.
- `FuzzExtract`: a 60s run finds nothing.

Import pipeline (ADR 0005):
- **Schema** (goose owns all of it, River's tables included):
  - users;
  - the shared cache: `sources` per canonical URL, `source_snapshots` with gzip bodies, `extractions` per snapshot and extractor version;
  - per user: `documents` (one per owner and source, `version` for later edits) and `imports` (queued|fetching|extracting|done|failed, stable `error_code`).
- **Owner scoping:** every documents/imports query takes `owner_id`, and a static test enforces it; a composite key stops an import pointing at another user's document; isolation is tested in the store, worker and API.
- **Canonical URLs** (`internal/canon`): normalized, tracking parameters dropped; a page's canonical link counts only on its own host.
- **Worker** (`cmd/worker`, `internal/importer`, River v0.51.0, queue `imports`, 4 workers, 60s per attempt):
  - `RONOTE_SOURCE_TTL` (24h) skips fetching; past it a conditional GET, where a 304 reuses the snapshot;
  - extraction always runs and reuses the stored row for the same extractor version;
  - document, done status and `JobCompleteTx` commit in one transaction, so duplicate or retried jobs make one document;
  - permanent errors fail at once; transient ones (5xx, 429 with Retry-After, timeouts, robots.txt 5xx) retry up to 3 attempts.
- **API** (`api/openapi.yaml`, OpenAPI 3.1, spec-first with oapi-codegen v2.8.2):
  - `POST /api/v1/imports` (202, or 200 with the existing document), `GET /imports/{id}`, `GET /imports/{id}/events` (SSE), `GET /documents` (cursor pages), `GET /documents/{id}`;
  - errors use one envelope with stable codes;
  - the spec's IR schema maps onto `internal/ir`, and every golden extraction must validate against it.
- **Progress:** a trigger NOTIFYs `import_status` in the same transaction; one LISTEN connection per API process fans out to SSE streams, with a 15s heartbeat.
- **Identity until P16:** dev and test inject `RONOTE_DEV_USER_EMAIL`; without it, and always in staging and prod, `/api/v1` answers 401.
- **Tests that catch regressions:** three deliberate breaks were each caught.
  - Dropping the one-document-per-owner-and-source constraint.
  - Trusting a cross-host canonical, which let a copy replace the real page for another user.
  - Retrying permanent errors.
- **Found by the demo:** go.dev's robots.txt answered 503 for a moment, and imports failed as if disallowed. Fixed: a robots.txt 5xx is now retried.

Unchanged: `/healthz`, `/readyz`, testcontainers integration tests, and the web app (P04 tokens, shell, styleguide).

## Open questions

- A user's document keeps the IR it was made from; re-importing returns it. Syncing to a newer snapshot belongs with editing (P10+).
- The fetch rate limit is per worker process; more than one worker process needs a shared limit.
- Pages of a recognised framework in another layout, such as a Docusaurus blog post, fall back to generic with `adapter_fallback`.

## Next step

P09: the web app's import and reading UI on top of this API, with TypeScript types generated from `api/openapi.yaml`.
