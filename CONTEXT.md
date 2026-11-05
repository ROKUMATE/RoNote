# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P12, highlights and notes: done.
- **The P11 clock question, answered and fixed.** River writes a job's times from the app's clock but claims jobs by the database's `now()`:
  - its pgx driver fills `scheduled_at` with `time.Now()`;
  - retries are `time.Now()` plus a backoff.

  With the podman VM 19 minutes behind the Mac, every new job sat 19 minutes in the database's future. Migration 00006's trigger moves those times onto the database's clock and puts the job in `available`, so the claim is the only comparison that decides when a job runs (ADR 0005). `TestJobsRunWhateverTheAppClock` fakes a half-hour offset either way; it failed both ways before the fix.
- **`make doctor`** checks the container VM's clock is within 2s of the host's, and prints the fix (`podman machine ssh sudo chronyc makestep`) when it is not.
- **Highlights and notes** (ADR 0007):
  - stored outside DocContent, owner-scoped, through an annotations API;
  - anchored by position, then quote, then a fuzzy match, else unplaced;
  - drawn in prose and in code;
  - notes in the right margin, or under the highlight below 1100px;
  - a list of every highlight, with recovery for unplaced ones.
- **Time to interactive** on Effective Go, production build, median of 5 cold loads at 1280×900 on this Mac:
  - 0 highlights: interactive 97 ms, LCP 128 ms;
  - 300 highlights, 30 with notes: interactive 110 ms, LCP 128 ms.

  That is +13 ms, against a limit of +50; it is also 11 ms under P10's 121 ms. All 300 were placed, none unplaced.
- **In the browser, on Effective Go:**
  - a selection across two adjacent paragraphs made one highlight in each;
  - a note on a third highlight showed in the margin;
  - typing at the start of a highlighted paragraph re-anchored its highlight (saved as version 2);
  - after a reload, the same words were highlighted and the note was in its place, and every stored selector quotes its block's stored text exactly.
- **Bundle, gzip:**
  - initial JS 119.8 kB of 150;
  - document route 131.9 kB of 135 (129.6 in P11);
  - highlighter core 54.5 kB of 60;
  - the new highlight controls chunk 6.6 kB of 15;
  - CSS 9.7 kB of 20.
- **Checks:**
  - `make web-e2e` passes 63 tests;
  - `make test-live` passes all 11 pages;
  - `make test-integration`, `make test-perf` and `make doctor` pass.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (ADR 0001).
- Source is verbatim and code byte-identical (ADR 0002), enforced by `TestCodeIsVerbatim`.
- Commits are one line, `Type: Summary`; the hook (strict mode in CI) enforces it (ADR 0003).
- PRs merge by rebase only, so main keeps one-line commits.
- CI runs on the pinned `ubuntu-26.04` image. `.github/actionlint.yaml` declares that label until actionlint knows it (rhysd/actionlint#682).
- License: MIT. Agents never push. Run `make check` unpiped and read its exit code.
- Timing tests check growth (2n against n), never wall time, and run only in `make test-perf`: tagged `perf`, one package at a time, no race detector. `make check` includes it.
- `make doctor` checks the local setup: so far, that the container VM's clock is the host's.

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
  - annotations (since P12, ADR 0007):
    - `GET` and `POST /documents/{id}/annotations`;
    - `PATCH` and `DELETE /annotations/{id}`, with a 409 carrying `current_version`;
    - `POST /documents/{id}/annotations/reanchor`, a batch, all or nothing;
    - every write checks its selector against the stored content, with the document row locked: `unknown_block`, `quote_mismatch`, `annotation_limit` (2000 per document);
  - `POST /documents/resolve` (since P11): up to 200 URLs, each answered in order with its canonical URL and the caller's document, if any. It canonicalizes like imports and matches like an import's "already in your library" (`ResolveDocuments`, one `unnest` query, owner-scoped);
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
    - anything else invalid answers `invalid_content`;
    - `code_langs` (since P11), optional: each key is a code block's id, at any depth, and each value one of the spec's 30 `CodeLang`s, else `invalid_content`. An optional field like this needs no new `doc_version` (ADR 0006).
  - **Revert:** `POST /documents/{id}/blocks/{blockId}/revert` puts the extraction's block back exactly and bumps the version, keeping the chosen code languages.
  - `GET /documents/{id}` returns the extraction's warnings next to the content.
- **One clock for jobs** (since P12, migration 00006): a trigger on `river_job` moves new jobs' and retries' times from the app's clock onto the database's, and puts them in `available`.
  - River's stuck-job rescue still compares against the app's clock; a skew over an hour can rescue a running job early, and imports are idempotent.
  - RoNote never snoozes jobs.
- **Annotations** (`internal/annotation`, ADR 0007):
  - one row per highlight: `block_id`, `kind` (`highlight`), `selector` (`{position: {start, end}, quote: {exact, prefix, suffix}}`), an optional `note` of at most 5000 characters, and a `version`;
  - offsets count UTF-16 code units of the block's plain text, the runs of a heading or paragraph joined, or a code block's text (`doccontent.BlockText`), and `testdata/anchoring/blocktext.json` keeps Go and TypeScript agreeing on it;
  - the composite key `(document_id, owner_id)` ties each to a document of the same owner, gone with it.
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
- **Highlights and notes** (since P12, ADR 0007):
  - **Anchoring** (`anchoring.ts`, pure):
    - position if the quote is still there;
    - else the occurrence of the quote whose surroundings agree most, then the nearest;
    - else the closest fuzzy match by edit distance (Sellers' algorithm with Ukkonen's cutoff), if at least 0.75 similar; quotes under four characters never match fuzzily;
    - else unplaced.
  - **Drawing:** a ProseMirror plugin (`highlights.ts`) anchors every highlight on each change and draws prose ones as `mark.hl` decorations, never marks, so nothing is an edit. Code views cut Shiki's tokens, or the plain text, at highlight edges, and the text stays the stored text.
  - **Saving moves:** after each content save and each revert, `ReanchorSync` re-anchors every highlight against the content just stored and sends the moved selectors in one batch, a second later, with Autosave's retry schedule. A conflict reloads and tries again.
  - **Creating:** pending edits are saved first, so the server has the text the selection quotes. Creating, a note, Remove and Undo go through `useAnnotations`.
  - **Controls** (`annotations/`, a lazy chunk loaded once the editor is up):
    - a toolbar over a selection, Highlight and Highlight + note, with one highlight per block for a selection across blocks; it also reads a selection inside one code block;
    - Mod+Shift+H highlights, Alt+F10 moves into the toolbar;
    - a popover with the note and Remove, and "Highlight removed · Undo" for 5s;
    - notes portalled into the shell's Notes aside, level with their highlights and stacked when they would overlap; without the aside (below 1100px), a note icon beside the text;
    - "Highlights (n)" in the header: every highlight in the order of the text (Jump to, note, Remove), then the unplaced ones (Re-apply, Remove). Re-apply turns the toolbar into Place here.
- **Same-domain links** (since P11, ADR 0006):
  - **Status:** the document page resolves its same-domain hrefs once, deduplicated, in batches of 200, as a query under the documents key, so a finished import refreshes it. In the editor, status shows as decorations, never marks, so nothing counts as an edit. In locked blocks, the IR renderer reads the same map from context. A link already in the library is underlined; one not imported yet gets a dotted underline and a "↓". External links open a new tab.
  - **Click** (`follow.ts`, `useFollow.ts`):
    - a plain press is stopped before the editor, so the caret stays where it was, and the link's text stays editable from the keyboard;
    - a link in the library opens its document;
    - otherwise it imports, with the steps in a small status box under the link, through the same `ImportTracker`;
    - a failure shows the dialog's sentence and "Open original";
    - Cmd/Ctrl-click and middle-click open the original.
  - **Arrival:** a `#fragment` scrolls to its heading and puts the caret there (`useScrollAnchor`); an unknown fragment does nothing.
  - **Back:** returns to the same place, recorded per history entry as the top-level block at the top of the viewport and its offset, since unrendered blocks keep placeholder heights.
- **Dev:** Vite proxies `/api` to `RONOTE_API_ORIGIN` (default the API's `127.0.0.1:8080`) and streams, so the event stream works through it.
- **End-to-end:** `make web-e2e` recreates `ronote_e2e` in the dev stack's Postgres (CI: a service container), builds the API and worker, and Playwright starts them with the test-only environment (`RONOTE_FETCH_ALLOW_PRIVATE=true`, user `e2e@ronote.local`), plus `e2e/fixture-site.ts`, which serves golden fixtures on `localhost:18090` (`/pages/`, `/slow/` 3s later, `/private/` behind robots.txt; since P11 `/linked/` for the two linked pages in `e2e/fixtures`, which carry the test's `?run=` into their links, and `/files/sample.pdf`; 404 otherwise). Every test imports a URL of its own, so tests do not depend on each other. axe runs on the library, the dialog (form and progress) and a document in light and dark, with reduced motion so the dialog is never measured mid-fade.
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
- **Highlighting:** Shiki core with the JavaScript regex engine, as a lazy chunk. A grammar loads per language once a block is within 600px of the viewport. The six code colours are tokens, in light and dark. A block without a declared language stays plain unless the user chooses one. Tokens are kept with their language, so a new choice never shows stale colours.
- **Code languages** (since P11):
  - The language label is a button opening a listbox (`CodeLangPicker.tsx`): arrows, Home/End, a letter for the next language starting with it, Enter, and Escape. Its block is exempt from `content-visibility` while the list is open, so the list is not clipped.
  - The page's language comes first, marked "(page)", and choosing it takes the user's choice back. A choice of the user's shows as "Go · yours" in `--color-gen`.
  - "Use for all unlabelled code in this document" covers every code block with no page language and no choice yet.
  - `useEditing` holds the choices and autosaves them with the content. A deleted block's choice is left out of the save.
  - Copy still copies the stored text.
- **Insert:** "+" below the block under the pointer or cursor, or "/" in an empty paragraph, opens a menu of exactly the six user kinds.
- **Autosave** (`save.ts`):
  - saves 1s after the last change, and at once on blur or when the page is hidden (keepalive);
  - the header shows Saving… / Saved / Offline, retrying, with backoff for network and 5xx errors;
  - a 409 shows "This document changed in another tab." with Reload, and saving stops;
  - a refused save says why.
- **Outline:** h2 and h3 in the sidebar (portalled there from the lazy chunk). The current heading is the last one above a third of the viewport, measured on every scrolled frame. A click scrolls there and puts the caret in the heading.
- **Long documents:** `content-visibility: auto` on code, tables, tabs and images only. Applied to text blocks, Chrome misplaced the caret in a just-created paragraph under load, and typing landed in the wrong place.
- **Load time,** measured in P10 on Effective Go (500 top-level blocks, 153 code blocks), production build, median of 5 cold loads on this Mac:
  - without throttling, LCP 108 ms and interactive 121 ms;
  - at 4x CPU throttling, 336 ms and 368 ms.
- **Budgets:** `apps/web/scripts/budget.ts` replaced `scripts/web-budget.sh` and reads Vite's manifest per chunk group (ADR 0006).
- **Tests that catch regressions:** four deliberate breaks were each caught.
  - The server accepting changed source code: the Go rule and API tests, and the e2e forge test (200 instead of 422).
  - The editor not marking edits: five unit tests, and the e2e test whose edit never saves.
  - The mapping losing `title_link`: the round trip of both Kubernetes goldens.
  - A save without a version: `tsc`, the page test (version undefined), and the e2e test.
- **Tests that catch regressions, P11:** four deliberate breaks were each caught.
  - Resolve without its `owner_id` predicate: the store integration test, where another user's document leaked.
  - The fragment dropped after navigation, in two places:
    - the scroll on arrival: the fragment page test and the e2e `#usage` test;
    - the link's hash on navigation: the follow page test and the same e2e test.
  - `code_langs` accepting any block id: the doccontent tests (a paragraph, top-level and nested) and the API's 422 test.
  - The quadratic extraction writer: `make test-perf` at 3.8x.
- **Tests that catch regressions, P12:** four deliberate breaks were each caught.
  - Trusting a stored position without checking its text: nine anchoring tests, and the e2e test that types before a highlight.
  - Deleting unplaced highlights instead of listing them: two page tests, and the e2e test that deletes highlighted words.
  - The API accepting a quote that is not the text: the rule's test, three handler tests and the store's integration test.
  - Without the clock trigger: `TestJobsRunWhateverTheAppClock`, both ways.
- **Found while building:**
  - P12: the highlight toolbar waited for Tiptap's focus event, which a browser window without focus never sends; it now shows for any selection until focus goes elsewhere.
  - P12: the lazy controls could miss a selection made between their first render and their listeners; they read it again once listening.
  - P12: an absolute-time test for fuzzy anchoring (under 50 ms) was planned and dropped, as AGENTS allows timing tests only in `make test-perf`. A 2 kB quote in a 10 kB block takes about 50 ms here, and only a highlight whose own text changed takes the fuzzy step at all.
  - P11: going back landed about 90 px off. Placeholder heights of not-yet-rendered blocks above the reader shifted the pixel offset, so places are now kept by block.
  - P11: switching a highlighted block to plain text kept the old colours. Tokens are now kept with their language.
  - P11: in Vitest, two dynamic imports of a mocked module started in one tick gave the second one the real module. Tests bring one block into view at a time.
  - Tiptap's `useEditor` reapplied options rebuilt on each render, which redrew the document mid-typing; the options are now built once.
  - The outline lost track after a fast jump (fixed, test first).
  - Tiptap's link extension pulled in linkifyjs, which is gone.

Unchanged: `/healthz`, `/readyz`, the P04 tokens and styleguide, the extractor and pipeline.

## Open questions

- go.dev declares no code languages. RoNote still never guesses one; the user can choose, per block or for all unlabelled code at once.
- Link status is resolved when a document opens and again after an import finishes. A page imported from another tab shows as not imported until then, and following it opens the existing document.
- Following a link that fails leaves its message under the link until the next link is followed.
- After macOS sleeps, the podman VM's clock can lag for a few minutes. Jobs no longer wait on it (P12), but tests that compare a database time with the app's, such as a user's `created_at` "about now", still fail meanwhile. `make doctor` says so and prints the fix.
- `TestFetchRateLimitsEachHost` failed twice inside a full `make check` while the machine was at load average 20 or more. It times requests where the test server receives them, not where the limiter lets them go, and passed 6 of 6 alone. Measuring at the limiter would make it robust.
- Highlights go in prose the editor edits, and in code; not in table cells, captions, or prose inside tabs, which are drawn read-only.
- Jump to puts the caret at a highlight's start rather than selecting it, so the toolbar does not offer to highlight it again.
- A highlight's note is plain text; links and formatting in notes are for later.
- A user's document keeps the IR it was made from; re-importing returns it. Syncing to a newer snapshot will have to merge user blocks and edits, and DocContent keeps what that needs.
- A deleted source block can only come back by undo in the same session; there is no "restore from source" for missing blocks yet.
- Images inside source blocks are locked like code, tables and tabs, a step beyond the P10 brief, because the editor shows them as atoms.
- The "edited" label sits above the block, not in the notes margin, which annotations (P12+) will use.
- The fetch rate limit is per worker process; more than one worker process needs a shared limit.
- Pages of a recognised framework in another layout, such as a Docusaurus blog post, fall back to generic with `adapter_fallback`.
- A failed or lost import is shown until dismissed, but only in the session that started it.
- The URL length check counts characters in the browser and bytes on the server.

## Next step

P13, per its brief. v1 still lacks running code blocks, and printed-PDF import, which the PDF link's message already promises.
