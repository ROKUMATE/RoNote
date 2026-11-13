# AGENTS.md

Rules for any coding agent working in this repo. Read CONTEXT.md before you start and rewrite it before you finish. CONTEXT.md stays within 80 lines and 6 KB, this file within 150 lines and 10 KB; `make lint` checks both.

## Product

RoNote turns ONE documentation page (a URL) or a printed PDF into an editable study doc for IT and CS students. Source content is reproduced verbatim; nothing is AI-added. Students highlight, annotate and run code blocks. Same-domain links inside a page import on click; other-domain links stay plain links. Students search their library, and (P17) chat over it: answers cite blocks and are never written into documents (docs/adr/0011-retrieval.md).

Stack: Go backend, React + TypeScript + Tailwind v4 frontend, Postgres + pgvector, one monorepo. Repo: github.com/ROKUMATE/RoNote.

Out of scope for v1: site crawling, AI summaries, handwritten OCR, pseudocode conversion. Do not build them or leave hooks for them. Nothing generated ever enters DocContent.

## Repo layout

| Path | Holds |
|---|---|
| `cmd/` | `api`, `worker`; dev tools `extract`, `eval`, `fakeidp`, `fakeembed` |
| `internal/<pkg>/` | Go packages, one concern each |
| `internal/jobs/` | the jobs port (docs/adr/0009-jobs-port.md); only `riverjobs` may import River |
| `migrations/` | goose migrations; goose owns all schema, River's included |
| `api/` | `openapi.yaml`, which Go and TypeScript types are generated from |
| `apps/web/` | the web app; `runner/` is the origin code from documents runs on |
| `testdata/golden/` | extractor fixtures |
| `docs/` | ADRs, `decisions.md`, `open-questions.md`, and `agents/` reference |

Read `docs/agents/repo-layout.md` when looking for where something lives. Create a directory when its first file needs it, not before.

## Commands

| Command | What it does |
|---|---|
| `make setup` | point git at `.githooks`; once per clone |
| `make check` | `lint`, `test`, `test-perf`, `hooks-test`: CI's fast jobs. Run before pushing |
| `make lint` | `lint-go`, `lint-web`, `lint-docs` |
| `make lint-go` / `lint-web` | golangci-lint, deadcode, tidy, actionlint / Biome, tsc, knip |
| `make lint-docs` | CONTEXT.md and AGENTS.md within their sizes |
| `make test` | `test-go` (with `-race`) and `test-web` (Vitest) |
| `make test-perf` | timing tests alone (`perf` tag, one package at a time) |
| `make test-integration` | unit and integration tests on testcontainers |
| `make eval` / `eval-record` | the retrieval eval on recorded vectors, gated on `baseline.json` / re-record them through the model |
| `make test-perf-search` | search timing over large libraries; local only |
| `make test-live` | extract the live pages and check the RAG tutorial; network, never CI |
| `make hooks-test` | test the commit hook and the scripts |
| `make check-commits RANGE=main..HEAD` | check each commit message in strict mode |
| `make web-dev` | app on `127.0.0.1:5173` and the runner on `localhost:5174` |
| `make web-build` | production build and bundle budgets |
| `make web-e2e` | Playwright and axe against the full stack (`make dev-up` first) |
| `make run-api` / `run-worker` | the API on `127.0.0.1:8080` / the job worker |
| `make run-idp` | the fake GitHub sign-in on `127.0.0.1:8090` (`web-dev` starts it too) |
| `make stop` | end what `run-api`, `run-worker`, `run-idp` and `web-dev` started |
| `make dev-up` / `dev-down` | start / stop the dev stack (Postgres) |
| `make doctor` | check the container VM's clock, and that the pinned embedding model answers |
| `make generate` / `check-generated` | regenerate sqlc and OpenAPI output / fail if stale |
| `make migrate-up` / `migrate-new name=...` | apply / create goose migrations |
| `make extract url=...` | print one page's IR |

Read `docs/agents/commands.md` for every target and what it runs in full, and `docs/agents/containers.md` when touching the local stack, containers or `.env.dev`.

## Commits

- One line: `Type: Summary`. Types: Feat, Fix, Refactor, Perf, Test, Doc, Style, Build, CI, Chore, Sec.
- Summary is imperative and capitalized, has no trailing period, and the whole line is at most 72 chars.
- No body. No trailers of any kind.
- One logical change per commit. Tests go in the same commit as the code they test.
- Never use `--no-verify`, never amend, never push.
- Never pipe verification commands (make targets, tests, linters) through `tail`, `head`, `grep` or `less`: the pipeline hides the failure. Run `make check` unpiped and read its exit code.
- `.githooks/commit-msg` enforces the format (docs/adr/0003-commit-convention.md).

## Code

- No dead code, no commented-out code, no unused exports, params or deps.
- No interface with a single implementation unless it is a test seam at a package boundary.
- No TODO without an issue number. No copy-paste: extract on second use.
- When you replace code, delete the old path in the same commit.
- Shell scripts are POSIX sh, work with GNU and BSD tools, and pass `shellcheck -s sh`.
- The API is spec-first: change `api/openapi.yaml`, then `make generate`. Never hand-write a type the spec defines; error codes are spec enums.

## Frontend (`apps/web`)

- pnpm only, every version pinned exactly; Node pinned by `.nvmrc`.
- Browsers: Chrome/Edge 123+, Firefox 125+, Safari 17.5+ (the Vite build target).
- Colours, fonts, text sizes, radii and breakpoints come only from `src/styles/tokens.css`: no raw colours, no palette classes.
- Imported documents are styled through `.reader` in `src/styles/reader.css`, not utility classes. Code never shows ligatures.
- No component library. Radix Primitives only where the platform gives no accessible behaviour; Radix Tabs is the one in use.
- The editor is Tiptap's MIT core, one extension package per feature, never Tiptap Pro.
- Phosphor icons at 16px, imported one per file.
- Every `localStorage` access goes in try/catch and falls back to the default.
- Dev-only code sits behind `import.meta.env.DEV`.
- API types come only from `src/api/schema.d.ts`; calls go through `src/api/client.ts`, server state through TanStack Query, every documents query under one key.
- Import progress is read only through `subscribeImport` (`src/api/events.ts`); the imports a session follows live in `ImportTracker`, outside React.
- Every API error code has one plain sentence in `src/api/messages.ts`.
- TypeScript is strict. A Biome suppression needs a reason.

## Tests

- Every Feat ships tests. Every Fix starts with a failing test.
- Go tests are table-driven and run with `-race`.
- Integration tests sit behind the `integration` tag and use testcontainers through `internal/pgtest`: one migrated Postgres per package, and `DB.Reset` truncates every table, so DB tests in a package run serially. Every container a test starts is terminated in cleanup, failures included (`make test-clean` removes leftovers).
- Unit tests never touch the network or paid APIs; loopback `httptest` servers are fine.
- Extractor changes: `go test ./internal/extract -update` rewrites the goldens; review that diff like code. `TestCodeIsVerbatim` and `TestGoldenHasNoJunk` stay green, and `FuzzExtract` runs 60s before committing.
- Web: Vitest and Testing Library; Playwright with axe, zero violations in light and dark. No screenshot assertions.
- A test that must stop running is quarantined, never deleted: `test.fixme`, `test.skip` or `t.Skip`, with `Quarantined: #N` naming its issue. Every phase summary lists the quarantined tests.

## Verbatim rule

Source text is never rewritten. Code blocks stay byte-identical, enforced by `TestCodeIsVerbatim` (docs/adr/0002-verbatim-source.md, docs/adr/0004-ir-and-links.md). In users' documents, source code, tables, tabs and images are locked (docs/adr/0006-editor-and-doc-content.md).
- A code block's text is the page's copy payload when it has one, else the `<pre>` text without line numbers, copy labels, unselectable prompts and frame chrome.
- Visible text stays. Never rewrite code with patterns; the one exception, FastAPI's terminal blocks, is in ADR 0004.

## Reference: read when touching

- The IR or DocContent's shape: `docs/agents/ir-version.md`.
- A docs-framework adapter: `docs/agents/adapters.md`.
- `.github/workflows` or a target CI calls: `docs/agents/ci.md`.

## Working economically

- While iterating, run only the affected tests. Run `make check` once at HEAD (CI checks HEAD), at every commit only when asked.
- `make test-live` only when fetch or extract code changed.
- One deliberate break per prompt, on the riskiest rule, unless asked for more.
- Don't re-run a passing suite to double-check. Don't print passing test output.
- Final summary: at most 10 lines.

## Done means

- `make check`, run unpiped, exits 0.
- CONTEXT.md is rewritten for the new state.
- Every commit follows the format above.
