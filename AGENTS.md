# AGENTS.md

Rules for any coding agent working in this repo. Read CONTEXT.md before you start and rewrite it before you finish.

## Product

RoNote turns ONE documentation page (a URL) or a printed PDF into an editable study doc for IT and CS students. Source content is reproduced verbatim; nothing is AI-added. Students highlight, annotate and run code blocks. Same-domain links inside a page become "import on click"; other-domain links stay plain links.

Stack: Go backend, React + TypeScript + Tailwind v4 frontend, Postgres + pgvector, one monorepo. Repo: github.com/ROKUMATE/RoNote.

### v1 scope

- Import one page from a URL, or one printed PDF.
- Reproduce the source verbatim, with code blocks byte-identical.
- Highlight, annotate and run code blocks.
- Same-domain links import on click; other-domain links stay plain links.

### Out of scope for v1

Site crawling, AI summaries, chat, handwritten OCR, pseudocode conversion. Do not build them or leave hooks for them.

## Repo layout

| Path | Holds |
|---|---|
| `cmd/api/` | HTTP API server |
| `cmd/worker/` | background job runner |
| `internal/<pkg>/` | Go packages, one concern each |
| `migrations/` | Postgres schema migrations |
| `apps/web/` | React + TypeScript + Tailwind v4 frontend |
| `deploy/` | deployment config |
| `scripts/` | repo scripts |
| `testdata/golden/` | source pages and PDFs with their expected output |
| `docs/adr/` | architecture decision records |
| `.githooks/` | git hooks, installed by `make setup` |

Create a directory when its first file needs it, not before.

## Commands

| Command | What it does |
|---|---|
| `make help` | list targets (default target) |
| `make setup` | point git at `.githooks`, make the hook executable, set `pull.rebase true`; run once per clone |
| `make hooks-test` | test the commit message hook |
| `make tools` | install the pinned golangci-lint into `./bin` |
| `make lint` (P0x) | Go and TypeScript linters |
| `make test` (P0x) | unit tests; Go runs with `-race` |
| `make test-integration` (P0x) | integration tests (`integration` build tag, testcontainers) |
| `make e2e` (P0x) | Playwright end-to-end tests |
| `make dev` (P0x) | run API, worker and web locally |
| `make build` (P0x) | build the Go binaries and the web bundle |
| `make migrate` (P0x) | apply database migrations |

Targets marked (P0x) do not exist yet. The phase that adds a target removes its tag.

Go CLIs are pinned as `tool` directives in go.mod (`go get -tool`) and run with `go tool <name>`. golangci-lint is the exception: `make tools` installs the version pinned in the Makefile into `./bin`.

## Commits

- One line: `Type: Summary`.
- Types: Feat, Fix, Refactor, Perf, Test, Doc, Style, Build, CI, Chore, Sec.
- Summary is imperative and capitalized, has no trailing period, and the whole line is at most 72 chars.
- No body. No trailers of any kind.
- One logical change per commit. Tests go in the same commit as the code they test.
- Never use `--no-verify`, never amend, never push.
- `.githooks/commit-msg` enforces the format (docs/adr/0003-commit-convention.md).

## Code

- No dead code, no commented-out code, no unused exports, params or deps.
- No interface with a single implementation unless it is a test seam at a package boundary.
- No TODO without an issue number.
- No copy-paste: extract on second use.
- When you replace code, delete the old path in the same commit.
- Shell scripts are POSIX sh, work with GNU and BSD tools, and pass `shellcheck -s sh`.

## Tests

- Every Feat ships tests.
- Every Fix starts with a failing test.
- Go tests are table-driven and run with `-race`.
- Integration tests live behind the `integration` build tag and use testcontainers.
- Unit tests never touch the network or paid APIs.

## Verbatim rule

Source text is never rewritten. Code blocks stay byte-identical, enforced by TestCodeIsVerbatim from P06 (docs/adr/0002-verbatim-source.md).

## Done means

- `make lint` and `make test` are green.
- CONTEXT.md is rewritten for the new state.
- Every commit follows the format above.
- You end with a 5-line summary of what changed and what's next.
