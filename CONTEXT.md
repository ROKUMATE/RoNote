# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P14, housekeeping and the jobs port: done.
- **Code in documents scrolls sideways again,** inside the editor too: Tiptap's injected, unlayered `.ProseMirror pre` rule had made it wrap since P10.
- **Agent docs, state and rules only:**
  - CONTEXT.md went from 352 lines and 41,349 B to this;
  - AGENTS.md went from 229 lines and 23,255 B to 125 lines and 8,259 B;
  - decisions now live in `docs/decisions.md`, open questions in `docs/open-questions.md`, reference detail in `docs/agents/`;
  - `make lint-docs`, part of `make lint` and CI, keeps both docs within their sizes.
- **The jobs port** (ADR 0009, `internal/jobs`):
  - enqueue in the caller's transaction, deliver at least once;
  - `Permanent`, `RetryAfter`, and per-kind attempts, concurrency and timeout;
  - `riverjobs` on River is the default and the only package that imports River (depguard); `memjobs` runs in memory for unit tests;
  - one conformance suite, `jobstest`, runs against both.
- **Imports run through the port.** The handler is plain, its completion no longer shares the import's transaction, and a redelivered job leaves a finished import alone. Behaviour, error codes, events and the API are unchanged. The P08 curl demo (POST, SSE to done, GET the document) works.
- **Checks:** `make check` exits 0; `make test-integration` exits 0; `make web-e2e` passed 95, skipped 1 (quarantined).

## What exists

- **Go** (`cmd/api`, `cmd/worker`, `internal/*`): a spec-first API (`api/openapi.yaml`), and an import pipeline on the jobs port (fetch with SSRF guard and robots.txt, extract to IR with framework adapters, documents per owner). Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes.
- **Web** (`apps/web`):
  - the library, and an import dialog with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, integration tests on testcontainers, Playwright and axe end to end, CI on every push and pull request.
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port.

## Next step

The next phase, per its brief. v1 still lacks printed-PDF import; the runner's deployment is P18.

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
