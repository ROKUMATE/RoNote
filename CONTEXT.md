# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P16, retrieval: done (ADR 0011). Search and P17's chat are in scope; nothing generated enters DocContent.
- **Chunks** (`internal/search`): blocks under one heading path up to 1,400 characters, code never split, notes their own chunks; goldens for every fixture.
- **Embeddings** (`internal/embed`): Qwen3-Embedding-0.6B through Ollama's OpenAI-compatible API (`RONOTE_EMBED_URL`, `RONOTE_EMBED_MODEL`, `make doctor` checks the pinned build); a cache shared across users; `cmd/fakeembed` for e2e; recorded vectors for the eval.
- **Indexing** on the jobs port, which gained delayed jobs: each change search sees enqueues `index_document` in its transaction, 5 s later; the worker backfills. One edit re-embedded 1 chunk of 6.
- **Search:** `GET /api/v1/search`, words and meaning merged by RRF, owner-scoped, word matches alone (`degraded`) without the embedder. Exact scan, PLAIN storage: hybrid p50 13 ms, p95 21 ms at 25,000 chunks of one user among 200,000 (`make test-perf-search`).
- **Web:** Mod+K or Search opens the palette (its own 1.75 kB chunk): results by document, arrow keys, "This document", landing on the block with one flash.
- **Eval** (`make eval`, in CI): 14 documents, 321 chunks, 80 queries; hybrid recall@10 0.958, MRR@10 0.808 (meaning alone 0.951, 0.811; words alone 0.806, 0.513).
- **In the browser,** "how do I run things concurrently" answers from Effective Go's Concurrency chapter (top 5), Goroutines 4th; Effective Go never names `ServeMux`, so that search finds no code of it.
- **Deliberate break:** dropping the owner filter from the vector half was caught by the EXPLAIN test; results never leaked, as the last join re-checks the owner.
- **Checks:** `make check`, `make test-integration`, `make eval` and `make web-e2e` (113 passed, 1 skipped) pass.

## What exists

- **Go** (`cmd/api`, `cmd/worker`, dev tools in `cmd/`, `internal/*`): a spec-first API (`api/openapi.yaml`) behind GitHub sign-in, hybrid search over each library, and an import pipeline on the jobs port (fetch with SSRF guard and robots.txt, extract to IR with framework adapters, documents per owner). Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes.
- **Web** (`apps/web`):
  - the library, and an import dialog with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser;
  - the search palette.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, integration tests on testcontainers, Playwright and axe end to end, CI on every push and pull request.
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port;
  - 0010 sign-in and sessions, 0011 retrieval.

## Next step

P17, chat over the library, per its brief; it needs more than a similarity threshold to say "I don't know" (`docs/open-questions.md`). v1 still lacks printed-PDF import; the runner's deployment is P18, proxy headers P19.

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
