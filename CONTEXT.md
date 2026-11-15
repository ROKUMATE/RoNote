# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P17, chat over the library: built (ADR 0012), tested on the fake model. Not yet run against the real one: there is no API billing yet.
- **Port** (`internal/chat`): `Model`, one streamed call; `internal/chat/anthropic` adapts the Messages API (SDK v2.9.0, Claude Haiku 5.5, effort low, `max_tokens` 1024, tools and system prompt cached) and is the only package importing the SDK. `cmd/fakellm` serves the same subset for dev and e2e.
- **Grounding:** the question's top 8 chunks (hybrid search, the conversation's scope) as `search_result` blocks, a text per block (`chunks.block_texts`, migration 00013); `search_library` for up to 3 more rounds, always as the owner.
- **Citations** count only if they point at a result sent in the turn and a block range inside it; the blocks and text are RoNote's own copy. A reply with none shows "Not found in your library".
- **Budgets:** a 30k input-token cap a call (3 characters a token, oldest turns dropped first); 50 questions and 400k input tokens per user per day (`quota_exceeded`); `RONOTE_CHAT_ENABLED` kill switch (503 `chat_disabled`), on by default in dev and test, off in staging and prod until it has run against the real model. Every answer records its tokens, cost in micro-dollars and latency.
- **API:** conversations CRUD, `POST …/messages` streaming `delta`, `citation`, `tool`, `done`, `error`; one answer at a time (409 `conversation_busy`); answers outlive a disconnect; `POST …/stop` (per API process).
- **Web:** the chat panel (Mod+J, its own 6.1 kB chunk), beside the page from 1100px, a sheet below; scope chip, streaming with Stop, generated treatment, numbered citation chips that preview on hover or focus and land on their block; conversation list; questions left today.
- **Eval:** `make eval-chat` asks the 80 eval queries through chat and writes `testdata/retrieval/chat-report.md` from the real model only; dry-run against fakellm here. The search eval is unchanged: hybrid recall@10 0.958, MRR@10 0.808.
- **Deliberate break:** citations accepted without checking their source against what was sent: `TestCitations` caught both cases.
- **Checks:** `make check`, `make test-integration`, `make eval` and `make web-e2e` pass.

**Waiting on billing:** recording the adapter's streams (`go test ./internal/chat/anthropic -record`), `make eval-chat` and its committed report, and the real model's answers in the browser.

## What exists

- **Go** (`cmd/api`, `cmd/worker`, dev tools in `cmd/`, `internal/*`): a spec-first API (`api/openapi.yaml`) behind GitHub sign-in; hybrid search over each library and chat over it; an import pipeline on the jobs port (fetch with SSRF guard and robots.txt, extract to IR with framework adapters, documents per owner). Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes.
- **Web** (`apps/web`):
  - the library, and an import dialog with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser;
  - the search palette and the chat panel.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, integration tests on testcontainers, Playwright and axe end to end, CI on every push and pull request; the search eval in CI.
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port;
  - 0010 sign-in and sessions, 0011 retrieval, 0012 chat.

## Next step

Add API billing and run what waits on it, then P18, the runner's deployment, and P19, proxy headers. v1 still lacks printed-PDF import.

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
