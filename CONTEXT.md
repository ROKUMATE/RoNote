# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P06, fetcher, IR, generic extractor and verbatim golden tests: done.
- One documentation page can be fetched safely and turned into IR blocks with byte-exact code.
- It's not wired to the API or DB yet; P08 adds import jobs and persistence.
- `make extract url=...` shows the pipeline end to end.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (ADR 0001).
- Source is verbatim and code byte-identical (ADR 0002), enforced by `TestCodeIsVerbatim`.
- Commits are one line, `Type: Summary`; the hook (strict mode in CI) enforces it (ADR 0003).
- PRs merge by rebase only, so main's history keeps one-line commits. The red push run after the PR #1 merge commit was the reason; the repository is set to rebase merges only since P06.
- License: MIT. Agents never push. Run `make check` before pushing, unpiped, and read its exit code: a lint failure piped through `tail` once reached a commit.

IR (`internal/ir`, ADR 0004):
- Closed block set: heading, paragraph, list, code, table, callout, image, quote, divider. Inline runs carry marks and links (anchor, same_domain or external).
- `Validate` checks every rule.
- Block IDs come from sha256 of heading path, kind and text, with `-n` for repeats.
- Warnings: `low_confidence` (no main element), `parse_failed` (a page the HTML parser rejects, which gives an empty doc).

Fetcher (`internal/fetch`):
- Rules: http(s) only, ports 80 and 443 only, a 5 MB cap, `text/html` or XHTML only, charset decoded to UTF-8.
- Timeouts: 5s connect, 20s total.
- User agent: `RoNoteBot/0.1`.
- **SSRF guard:** runs at dial time, on redirects too (at most 5).
  - Always blocked: unspecified, multicast, reserved, IPv4-mapped IPv6 and 169.254.169.254.
  - Also blocked: loopback, private, link-local, CGNAT, ULA and NAT64, unless `AllowPrivateNetworks`. That option also lifts the port rule, and `RONOTE_FETCH_ALLOW_PRIVATE` may set it only in dev and test.
- **robots.txt:** RFC 9309 subset, cached per origin for 1h. 404/410 allow everything; 5xx and other statuses deny; 5xx answers are not cached.
- **Rate limit:** 1 request per second per host.
- **Conditional GET:** sends ETag and Last-Modified and returns `NotModified`.
- Typed errors.
- The config field is not read until P08.

Extractor (`internal/extract`, generic v1):
- Root: main, then article, then `[role=main]`, else body without header and footer, plus `low_confidence`.
- Fixed removal list (nav, aside, script, form, `[hidden]`, skip and permalink links, ...).
- Callouts come from class or role markers.
- Heading anchors: the heading's id, the opening section's id, an inner `a[id|name]`, then a slug.
- Linear time on adversarial input, with a test guarding it.
- Golden tests: 19 fixtures in `testdata/golden` (16 hand-written, 3 licensed excerpts in `SOURCES.md`).
- `FuzzExtract`: 10 committed seeds plus the fixtures. 60s runs find nothing.

Unchanged: the API (`/healthz`, `/readyz` with Postgres), the store and migrations, testcontainers integration tests, the web app (P04 tokens, shell, styleguide), and CI (P05).

## Open questions

- Generic extraction keeps in-page chrome that sits inside the content root, such as go.dev's breadcrumb list. P07's framework adapters are where site-specific cleanup belongs.

## Next step

P07: framework adapters on top of the generic extractor (detailed scope not yet written).
