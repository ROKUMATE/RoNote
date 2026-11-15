# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P18, printed PDF import (ADR 0013): built, recorded and run against the real docling in Chrome.
- **Upload:** `POST /api/v1/imports/pdf` (25 MB, `%PDF-`, the daily import quota), the same `Import` and event stream with a `converting` step. The same file twice is the same document.
- **Convert:** docling-serve 1.38.2 (CPU image by digest, no egress, an API key) lays out the PDF; pdftext (`deploy/pdftext`, docling's image) reads the text layer in docling's boxes: code by glyph columns, and monospace prose or pictures as code, prose labels out of code boxes. pdftext inspects first: password, unreadable, over 300 pages and scans fail at once with stable codes.
- **IR 2:** every PDF block records its page; `source.pdf` (filename, pages); `figures_omitted` with a count. Migration 00014 upgraded stored extractions; DocContent stays 1.
- **Storage:** a blob port (`internal/blob`: S3 via aws-sdk-go-v2, memory for tests), SeaweedFS in dev and CI, keys `pdf/<owner>/<sha256>`. PDF sources are their owner's alone.
- **Original:** `GET /api/v1/documents/{id}/original`, owner only (404), sandboxed CSP, nosniff, safe filename. Chrome 157 shows it and `#page=N` lands.
- **Deletion:** `DELETE /api/v1/documents/{id}` (and in the web app) takes notes, document conversations and a PDF's blob; account deletion takes `pdf/<owner>/`; a sweep removes leftovers.
- **Web:** Upload PDF in the import dialog (picker, drop, client checks), filename and pages as the source, "View page N" beside the block under the pointer or caret, the figures notice.
- **Verbatim report** (`testdata/pdf/report.md`): 56 code blocks of 5 printed golden pages, 46 exact, 10 whitespace only (a final newline, a space after an emoji), none other or missing. About 1 s a page.
- **Deliberate break:** `/original` without the owner check: `TestOriginalIsTheOwners` and the e2e 404 test caught it.
- **Checks:** `make check`, `make test-integration`, `make web-e2e`, `make eval` pass.

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

## Waiting on billing

Recording the chat adapter's streams (`go test ./internal/chat/anthropic -record`), `make eval-chat` and its report, the real model's answers in the browser. Chat stays off in staging and prod until then.

## Next step

Add API billing and run what waits on it; then the runner's deployment, and P19, proxy headers.

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
