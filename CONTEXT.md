# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P07, docs-framework adapters: done.
- Real pages from the doc sites students use come out clean and faithful. All 11 pages in `make test-live` pass:
  - each picks its adapter;
  - code found equals an independent count, and the verbatim check passes;
  - no junk, no warnings.
- Not wired to the API or DB yet. P08 adds import jobs and persistence.
- `make extract url=...` shows the pipeline end to end, and its text dump shows tabs and code titles.

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
  - Code has `lang` and an optional `title`, such as a file name or frame label.
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
- `TestCodeIsVerbatim`'s token walker applies the same rules independently, with per-adapter rules keyed by the chosen adapter.

Fetcher (`internal/fetch`):
- **Rules:** http(s) only, ports 80 and 443 only, a 5 MB cap, `text/html` or XHTML only, charset decoded to UTF-8.
- **Timeouts:** 5s connect, 20s total.
- **User agent:** `RoNoteBot/0.1`.
- **SSRF guard:** runs at dial time, on redirects too (at most 5).
  - Always blocked: unspecified, multicast, reserved, IPv4-mapped IPv6 and 169.254.169.254.
  - Also blocked: loopback, private, link-local, CGNAT, ULA and NAT64, unless `AllowPrivateNetworks`. That option also lifts the port rule, and `RONOTE_FETCH_ALLOW_PRIVATE` may set it only in dev and test.
- **robots.txt:** RFC 9309 subset, cached per origin for 1h. All 11 live pages allow RoNoteBot.
- **Rate limit:** 1 request per second per host.
- **Conditional GET:** sends ETag and Last-Modified.

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

Unchanged: the API (`/healthz`, `/readyz` with Postgres), the store and migrations, testcontainers integration tests, and the web app (P04 tokens, shell, styleguide).

## Open questions

- FastAPI's `termy` terminal blocks hold literal `<font color=…>` markup that FastAPI's own JS renders. It's kept verbatim, as the `<pre>` and its copy text hold it.
- A code title is plain text, so Kubernetes' link from the file name to the raw file is lost.
- Pages of a recognised framework that use another layout, such as a Docusaurus blog post, fall back to generic with `adapter_fallback`. Blog roots can be added when imports need them.

## Next step

P08: import jobs and persistence. A worker fetches and extracts a URL, stores the IR document, and the API exposes it.
