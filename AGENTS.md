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
| `make lint` | golangci-lint (`.golangci.yml`, includes `integration` files), then `go tool deadcode -test -tags integration ./...`, which must print nothing |
| `make fmt` | format Go code with gofumpt and goimports |
| `make test` | unit tests; Go runs with `-race` |
| `make test-integration` | unit and integration tests (`integration` build tag, testcontainers); fails if a test container is left behind |
| `make test-clean` | remove leftover testcontainers containers |
| `make e2e` (P0x) | Playwright end-to-end tests |
| `make run-api` | run the API on `127.0.0.1:8080`; `RONOTE_ENV` defaults to `dev` |
| `make dev` (P0x) | run API, worker and web locally |
| `make dev-up` | start the dev stack (`deploy/compose.dev.yml`) and wait until healthy; creates `.env.dev` if missing |
| `make dev-down` | stop the dev stack; the data volume is kept |
| `make dev-logs` | follow the dev stack logs |
| `make db-shell` | psql on the dev database |
| `make build` (P0x) | build the Go binaries and the web bundle |
| `make migrate-up` / `migrate-down` / `migrate-status` | apply, roll back the latest, or list goose migrations against `RONOTE_DATABASE_URL` |
| `make migrate-new name=...` | create the next sequential SQL migration in `migrations/` |
| `make sqlc` | regenerate `internal/store/sqlcdb` from `migrations/` and `internal/store/queries/`; commit the result |

Targets marked (P0x) do not exist yet. The phase that adds a target removes its tag.

### Local containers

- macOS uses podman machine (rootless); Linux and CI use Docker. `scripts/container.sh` picks whichever answers.
- `podman compose` needs a compose provider: `brew install docker-compose` (or podman-compose). `make dev-up` fails with a clear message when neither `podman compose` nor `docker compose` works.
- `.env.example` lists every variable. Copy it to `.env.dev` (gitignored); the Makefile loads it, its values override the shell environment, and `make VAR=value` still wins.

Go CLIs are pinned as `tool` directives in go.mod (`go get -tool`) and run with `go tool <name>`: deadcode, goose, sqlc. golangci-lint is the exception: `make tools` installs the version pinned in the Makefile into `./bin`.

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
- Integration tests live behind the `integration` build tag and use testcontainers through `internal/pgtest`: `pgtest.Main` in TestMain starts one migrated Postgres per package and `DB.Reset` truncates every table before a test, so DB tests in a package run serially.
- Ryuk is off on podman, so nothing reaps containers: every container a test starts must be terminated in cleanup, failures included. `make test-clean` removes leftovers.
- Unit tests never touch the network or paid APIs.

## Verbatim rule

Source text is never rewritten. Code blocks stay byte-identical, enforced by TestCodeIsVerbatim from P06 (docs/adr/0002-verbatim-source.md).

## Done means

- `make lint` and `make test` are green.
- CONTEXT.md is rewritten for the new state.
- Every commit follows the format above.
- You end with a 5-line summary of what changed and what's next.
