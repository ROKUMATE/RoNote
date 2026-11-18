# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P18.3, cleanup after P18 and the chat eval.
- **Chat says "not in your library" itself.** The model calls `not_in_library(missing, suggest)`; the message is `not_found` and shows both. A reply with text and no citation is `unsourced` ("No sources", never hidden); the markers a model writes into its text are counted, never rewritten (ADR 0012).
- **Chat has run against the real model** (Claude Haiku 5.5): `make eval-chat`, 80 queries on 2026-11-18, $0.069 (`testdata/retrieval/chat-report.md`; every answer in `chat-answers.jsonl`):
  - citations: 204 of 204 valid; hit rate 0.96 by section, 0.68 strict, against search's recall@10 of 0.96;
  - no false refusals; of 8 unanswerable questions 4 called the tool, 3 said so in text without it (unsourced) and n04 answered with citations;
  - e06 once wrote its own markers and was unsourced; asked 5 more times it cited every time;
  - first token p50 2.0 s, total p50 3.2 s; about $0.0009 a question.
- **The Anthropic key is `RONOTE_ANTHROPIC_API_KEY`**, like every other variable; a test fails when Go reads an unprefixed one.
- **Chat stays off by default in staging and prod**, on in dev and test; P19 turns it on with `RONOTE_CHAT_ENABLED=true` (ADR 0012).
- **Links inside highlights** pass axe in both themes: the dark accent is `#e6946d` and the dark highlight `#483c12` (4.6:1), and the styleguide and an e2e test have such a link.
- **P18, printed PDF import** (ADR 0013), built before this:
  - docling converts the PDF and pdftext reads its text layer into code; every PDF block records its page (IR 2, `source.pdf`);
  - a blob port (S3 in dev and CI, memory in tests) stores the original, shown at `/api/v1/documents/{id}/original` (owner only, sandboxed), and documents can be deleted;
  - the web app uploads PDFs and links "View page N";
  - the verbatim report (`testdata/pdf/report.md`): 56 code blocks, 46 exact, 10 differing in whitespace only.

## What exists

- **Go** (`cmd/api`, `cmd/worker`, dev tools in `cmd/`, `internal/*`): a spec-first API (`api/openapi.yaml`) behind GitHub sign-in; hybrid search over each library and chat over it; an import pipeline on the jobs port for pages (fetch with SSRF guard and robots.txt, extract to IR with framework adapters) and PDFs (docling and pdftext), documents per owner. Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes.
- **Web** (`apps/web`):
  - the library, and an import dialog for pages and PDFs with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click, and PDF page links;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser;
  - the search palette and the chat panel.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, recorded docling fixtures with goldens and the PDF verbatim report, integration tests on testcontainers (Postgres, SeaweedFS), Playwright and axe end to end against fake sign-in, embedder, model and docling, CI on every push and pull request; the search eval in CI.
- **Dev stack:** `make dev-up` (Postgres, SeaweedFS with its bucket), `make dev-up-docling` (the real docling and pdftext); `make doctor` checks them.
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port;
  - 0010 sign-in and sessions, 0011 retrieval, 0012 chat, 0013 PDF import.

## Next step

P19: proxy headers, the runner's deployment, and chat on by env in staging and prod. First, decide what to do about the model refusing in text instead of calling the tool (docs/open-questions.md).

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
