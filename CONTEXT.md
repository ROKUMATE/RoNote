# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P10, reader and editor: done.
- `/doc/$id` reads and edits a document in place: Tiptap (MIT core), with code, tables, tabs, images and dividers as locked atoms, autosave, edited marks and Revert to source (ADR 0006).
- Effective Go (500 top-level blocks, 153 code blocks), production build, median of 5 cold loads on this Mac:
  - without throttling, first paint of the document (LCP) 108 ms and interactive (editor editable, main thread idle) 121 ms;
  - at 4x CPU throttling, 336 ms and 368 ms.
- In the browser, Effective Go reads well, but its code is not highlighted: go.dev declares no language and RoNote never guesses one. vite.dev's guide shows the lazy highlighting: no grammar loads at the top of the page, and after scrolling 8 of its 9 blocks are highlighted, each language its own chunk, with every highlighted block spelling its stored text exactly.
- An edited Effective Go paragraph autosaved. Reverted, it matched its extraction block exactly and was marked unedited again.
- Bundle, gzip:
  - initial JS 119.4 kB of 150;
  - document route 126.1 kB of 135;
  - highlighter core 54.5 kB of 60;
  - CSS 9.2 kB of 20;
  - 42 Shiki language chunks, 273 kB in all, each loaded only when a page needs it.
- `make web-e2e` passes 35 tests. `make test-live` still passes all 11 pages.

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
- **Frozen at `ir_version` 1** since P08: extractions are stored as IR, and documents as DocContent, whose blocks are IR blocks. Any IR shape change needs a version bump and an upgrade path for both.
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
  - errors use one envelope; its codes are the `ErrorCode` enum, and an import's failure codes the `ImportErrorCode` enum, so Go uses generated constants and a classifier test keeps the worker's codes in the spec;
  - every timestamp is RFC 3339 in UTC (`Z`), whatever zone the database driver returns;
  - every operation declares its 500 `internal_error` answer; a contract test validates every response the API's tests produce against the spec, statuses included;
  - the spec's IR schema maps onto `internal/ir`, and every golden extraction must validate against it.
- **Progress:** a trigger NOTIFYs `import_status` in the same transaction; one LISTEN connection per API process fans out to SSE streams, with a 15s heartbeat.
- **Editing** (`internal/doccontent`, ADR 0006):
  - **DocContent v1** is `{doc_version: 1, blocks}`. Each top-level block is an IR block plus `origin` (source or user), and for source blocks `edited` and `source_block_id`. Migration 00005 converted stored documents; its down migration rebuilds the IR and keeps edits. Both directions are tested.
  - **Save:** `PATCH /documents/{id}` takes `{content, version}`. A stale version answers 409 `version_conflict` with `current_version`.
  - **What a save must keep:**
    - every source block names an extraction block, at most once (`unknown_source_block`);
    - code, tables, tabs and images inside a source block, at any depth, equal their extraction blocks byte for byte (`source_block_modified`);
    - an unedited source block equals its extraction block (also `source_block_modified`);
    - user blocks use only paragraph, heading, list, quote, callout and divider (`block_kind_not_allowed`, the spec's `UserBlockKind`);
    - anything else invalid answers `invalid_content`.
  - **Revert:** `POST /documents/{id}/blocks/{blockId}/revert` puts the extraction's block back exactly and bumps the version.
  - `GET /documents/{id}` returns the extraction's warnings next to the content.
- **Identity until P16:** dev and test inject `RONOTE_DEV_USER_EMAIL`; without it, and always in staging and prod, `/api/v1` answers 401.
- **Tests that catch regressions:** three deliberate breaks were each caught.
  - Dropping the one-document-per-owner-and-source constraint.
  - Trusting a cross-host canonical, which let a copy replace the real page for another user.
  - Retrying permanent errors.
- **Found by the demo:** go.dev's robots.txt answered 503 for a moment, and imports failed as if disallowed. Fixed: a robots.txt 5xx is now retried.

Web app (`apps/web`, since P09):
- **Types:** `src/api/schema.d.ts` is generated from `api/openapi.yaml` by openapi-typescript, run from `tools/openapi-ts`. That package exists because openapi-typescript needs TypeScript 5's compiler API, and the app compiles with TypeScript 7 (pnpm hands a package's peer from its parent, so overrides could not split the versions inside `apps/web`). `make generate` covers it, so CI's generated-code check does too. A Vitest test fails on any hand-written copy of a spec type.
- **Server state:** openapi-fetch (`src/api/client.ts`) and TanStack Query (`src/api/queries.ts`): an infinite query for the library (cursor, 20 a page), the sidebar's 8 newest, one document. Everything sits under the `documents` key; 4xx answers are not retried.
- **Progress:** `subscribeImport` wraps `EventSource`, closes at done or failed, and relies on the browser's reconnect, since the server starts each stream with the current status. `ImportTracker` holds the session's imports outside React; status only moves forward, so a reconnect's repeat changes nothing. On done it invalidates every documents query.
- **Import dialog:** a native `<dialog>`. The URL is checked like the server does (http or https, at most 2048 characters), but the server decides. After a 202 the steps show inline, with a polite status line; on done it opens the document. A 200 opens the document with "Already in your library". A failure gets one sentence per error code (`src/api/messages.ts`). Closing never cancels: the library shows the import as a pending row until it ends.
- **Library:** title, source host and imported date per row, "Load more" for the next cursor, still placeholder rows while loading, one sentence and Retry on error. The sidebar lists the 8 newest documents at the sidebar breakpoint.
- **Document page (`/doc/$id`):** title, source link (new tab, `noopener noreferrer`), host, imported date, block counts by kind (nested blocks included), and a notice when the extraction fell back to the generic rules (`adapter_fallback`, `low_confidence`).
- **Dev:** Vite proxies `/api` to `RONOTE_API_ORIGIN` (default the API's `127.0.0.1:8080`) and streams, so the event stream works through it.
- **End-to-end:** `make web-e2e` recreates `ronote_e2e` in the dev stack's Postgres (CI: a service container), builds the API and worker, and Playwright starts them with the test-only environment (`RONOTE_FETCH_ALLOW_PRIVATE=true`, user `e2e@ronote.local`), plus `e2e/fixture-site.ts`, which serves golden fixtures on `localhost:18090` (`/pages/`, `/slow/` 3s later, `/private/` behind robots.txt, 404 otherwise). Every test imports a URL of its own, so tests do not depend on each other. axe runs on the library, the dialog (form and progress) and a document in light and dark, with reduced motion so the dialog is never measured mid-fade.
- **Tests that catch regressions:** three deliberate breaks were each caught.
  - Dropping one error-code message: `tsc` and the messages test.
  - Leaving the event stream open after done: the subscribeImport test sees the reconnect's second done.
  - Skipping the invalidation after done: the dialog tests, and the e2e test whose sidebar never lists the new document.

Reader and editor (`apps/web/src/document`, since P10; `/doc/$id` is a lazy chunk):
- **Mapping:** `mapping.ts` converts IR ⇄ Tiptap JSON, and every golden fixture round-trips through a real editor unchanged. Unedited source blocks are saved exactly as loaded, never re-serialized.
- **Schema** (`schema.ts`):
  - paragraph, heading (with its anchor), list (one node, `ordered`), quote and callout (its title fixed) are editable;
  - code, table, tabs, image and divider are atoms carrying their IR block, with no paste rules;
  - links are a small mark of RoNote's own: http(s) or a `#fragment`, the kind kept.
- **Identity plugin** (`identity.ts`):
  - a top-level source block is edited exactly when it differs from how it loaded, so undo clears the mark; a block that loaded edited stays edited until reverted;
  - every block gets a unique id, and the half a split copies becomes a user block with a UUIDv7;
  - pasting drops locked blocks.
- **Edited marks:** an edited source block has a thin `--color-gen` rule and, above it, "edited" and "Revert to source". Revert asks first, then saves pending edits and calls the API.
- **Views:**
  - code: title (linked from `title_link`), language label, Copy (the stored text), the focusable scrolling `pre`;
  - tabs: Radix Tabs, panels drawn by a read-only IR renderer;
  - images: lazy, `referrerpolicy="no-referrer"`, alt text;
  - links: anchors move the caret to their heading; other links open a new tab with `noopener noreferrer`.
- **Highlighting:** Shiki core with the JavaScript regex engine, as a lazy chunk. A grammar loads per language once a block is within 600px of the viewport. The six code colours are tokens, in light and dark. A block without a declared language stays plain.
- **Insert:** "+" below the block under the pointer or cursor, or "/" in an empty paragraph, opens a menu of exactly the six user kinds.
- **Autosave** (`save.ts`):
  - saves 1s after the last change, and at once on blur or when the page is hidden (keepalive);
  - the header shows Saving… / Saved / Offline, retrying, with backoff for network and 5xx errors;
  - a 409 shows "This document changed in another tab." with Reload, and saving stops;
  - a refused save says why.
- **Outline:** h2 and h3 in the sidebar (portalled there from the lazy chunk). The current heading is the last one above a third of the viewport, measured on every scrolled frame. A click scrolls there and puts the caret in the heading.
- **Long documents:** `content-visibility: auto` on code, tables, tabs and images only. Applied to text blocks, Chrome misplaced the caret in a just-created paragraph under load, and typing landed in the wrong place.
- **Budgets:** `apps/web/scripts/budget.ts` replaced `scripts/web-budget.sh` and reads Vite's manifest per chunk group (ADR 0006).
- **Tests that catch regressions:** four deliberate breaks were each caught.
  - The server accepting changed source code: the Go rule and API tests, and the e2e forge test (200 instead of 422).
  - The editor not marking edits: five unit tests, and the e2e test whose edit never saves.
  - The mapping losing `title_link`: the round trip of both Kubernetes goldens.
  - A save without a version: `tsc`, the page test (version undefined), and the e2e test.
- **Found while building:**
  - Tiptap's `useEditor` reapplied options rebuilt on each render, which redrew the document mid-typing; the options are now built once.
  - The outline lost track after a fast jump (fixed, test first).
  - Tiptap's link extension pulled in linkifyjs, which is gone.

Unchanged: `/healthz`, `/readyz`, the P04 tokens and styleguide, the extractor and pipeline.

## Open questions

- go.dev declares no code languages, so its pages are not highlighted. Inferring a language was turned down: RoNote never guesses.
- A user's document keeps the IR it was made from; re-importing returns it. Syncing to a newer snapshot will have to merge user blocks and edits, and DocContent keeps what that needs.
- A deleted source block can only come back by undo in the same session; there is no "restore from source" for missing blocks yet.
- Images inside source blocks are locked like code, tables and tabs, a step beyond the P10 brief, because the editor shows them as atoms.
- The "edited" label sits above the block, not in the notes margin, which annotations (P12+) will use.
- The fetch rate limit is per worker process; more than one worker process needs a shared limit.
- Pages of a recognised framework in another layout, such as a Docusaurus blog post, fall back to generic with `adapter_fallback`.
- A failed or lost import is shown until dismissed, but only in the session that started it.
- The URL length check counts characters in the browser and bytes on the server.

## Next step

P11: same-domain links import on click, inside the reader built in P10.
