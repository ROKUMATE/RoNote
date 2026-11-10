# CONTEXT.md

The project's state, in at most 80 lines and 6 KB. Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P13b, doctest checks, JavaScript runs and run polish: done.
- **Check examples** runs a Python transcript through doctest in the session: each example Passed, Failed (with a line diff) or a Known difference (an object's address, line-end spaces or blank lines, an exception's wording between versions).
- **JavaScript runs** in a worker of its own on the runner origin: one session per document, top-level `await`, devtools-like console output; Stop and the limits end its session. One limits module serves Python and JavaScript.
- **Polish:** unlabelled blocks ask for a language; Mod+Enter runs the focused block; the header shows each runtime's session with its own Restart; Python warms up on the first hover or focus of its Run button.
- **Measured:** cold click to first output 745 ms, 226 ms after a 0.5 s hover, 13 ms after 2 s. Run chunk 13.2 kB of 20; document route 132.4 kB of 135.
- **Checks:** `make check` exits 0; `make web-e2e` 94 passed, 1 skipped (quarantined).

## What exists

- **Go** (`cmd/api`, `cmd/worker`, `internal/*`): spec-first API (`api/openapi.yaml`), import pipeline on River (fetch with SSRF guard and robots.txt, extract to IR with framework adapters, documents per owner), event streams from Postgres LISTEN/NOTIFY, editing with locked source blocks, highlights and notes.
- **Web** (`apps/web`): library, import dialog with live progress, the document reader and editor (Tiptap), same-domain links that import on click, code highlighting and language choice, highlights and notes, code that runs in the browser.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, integration tests on testcontainers, Playwright and axe end to end, CI on every push and pull request.
- **ADRs:** 0001 stack, 0002 verbatim source, 0003 commits, 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent, 0007 annotations, 0008 in-browser code execution.

## Next step

The next phase, per its brief. v1 still lacks printed-PDF import; the runner's deployment is P18.

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
