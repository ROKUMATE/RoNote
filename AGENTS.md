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
| `cmd/extract/` | dev tool: fetch one page and print its IR (`make extract`) |
| `internal/<pkg>/` | Go packages, one concern each |
| `migrations/` | Postgres schema migrations |
| `apps/web/` | React + TypeScript + Tailwind v4 frontend |
| `deploy/` | deployment config |
| `scripts/` | repo scripts |
| `internal/ir/` | the document model every importer produces (docs/adr/0004-ir-and-links.md) |
| `internal/fetch/` | the only way to fetch a user-supplied URL: SSRF guard, limits, robots.txt, rate limit |
| `internal/extract/` | HTML to IR extractor: generic rules, refined by a framework adapter when one matches |
| `internal/extract/adapter/` | docs-framework adapters as configuration: detection, content root, chrome, callouts, tabs and code rules |
| `testdata/golden/` | extractor fixtures: `input.html`, `meta.json`, `expected.json`; third-party excerpts are listed in `SOURCES.md` |
| `docs/adr/` | architecture decision records |
| `.githooks/` | git hooks, installed by `make setup` |

Create a directory when its first file needs it, not before.

## Commands

| Command | What it does |
|---|---|
| `make help` | list targets (default target) |
| `make setup` | point git at `.githooks`, make the hook executable, set `pull.rebase true`; run once per clone |
| `make hooks-test` | test the commit message hook (normal and strict mode) and `scripts/check-commits.sh` |
| `make check-commits RANGE=main..HEAD` | check every commit message in `RANGE` in strict mode; in CI the event sets the range (`scripts/commit-range.sh`). `scripts/check-commits.sh` exits 0 when all pass, 1 when any is bad (report on stderr), 2 on a usage or internal error |
| `make tools` | install the pinned golangci-lint into `./bin` |
| `make check` | `lint`, `test` and `hooks-test`: what CI's fast jobs run. Run it before pushing |
| `make lint` | `lint-go` and `lint-web` |
| `make lint-go` | golangci-lint (`.golangci.yml`, includes `integration` and `live` files), then `go tool deadcode -test -tags integration,live ./...`, which must print nothing, then `go mod tidy -diff`, then `go tool actionlint` on `.github/workflows` |
| `make lint-web` | `biome check --error-on-warnings`, `tsc -b`, `knip`, all with zero findings |
| `make fmt` | format Go code with gofumpt and goimports |
| `make test` | `test-go` and `test-web` |
| `make test-go` | Go unit tests with `-race` |
| `make test-web` | web unit tests (Vitest, jsdom) |
| `make test-integration` | unit and integration tests (`integration` build tag, testcontainers); fails if a test container is left behind |
| `make test-live` | extract the real pages listed in `internal/extract/live_test.go` over the network and print a table: adapter, blocks, code found vs an independent count, verbatim check, warnings, junk. Needs the network, so it never runs in CI |
| `make test-clean` | remove leftover testcontainers containers |
| `make web-install` | `web-deps` and `web-browsers` |
| `make web-deps` | `pnpm install --frozen-lockfile` |
| `make web-browsers` | Playwright's Chromium; `WITH_DEPS=1` also installs its system packages (CI) |
| `make check-node-version` | print the Node version and fail unless it is `.nvmrc`'s |
| `make web-dev` | Vite dev server on `127.0.0.1:5173`; the styleguide is at `/_styleguide` |
| `make web-build` | production build into `apps/web/dist`; fails if styleguide code ends up in it or the gzip size passes the budget in `scripts/web-budget.sh` (JS 150 kB, CSS 20 kB) |
| `make web-e2e` | Playwright end-to-end and axe tests in Chromium against the dev server; kept out of `make test` |
| `make run-api` | run the API on `127.0.0.1:8080`; `RONOTE_ENV` defaults to `dev` |
| `make extract url=... [format=text]` | fetch one page with the production fetcher rules and print its IR as JSON, or as readable text |
| `make dev` (P0x) | run API, worker and web locally |
| `make dev-up` | start the dev stack (`deploy/compose.dev.yml`) and wait until healthy; creates `.env.dev` if missing |
| `make dev-down` | stop the dev stack; the data volume is kept |
| `make dev-logs` | follow the dev stack logs |
| `make db-shell` | psql on the dev database |
| `make build` (P0x) | build the Go binaries and the web bundle |
| `make migrate-up` / `migrate-down` / `migrate-status` | apply, roll back the latest, or list goose migrations against `RONOTE_DATABASE_URL` |
| `make migrate-new name=...` | create the next sequential SQL migration in `migrations/` |
| `make sqlc` | regenerate `internal/store/sqlcdb` from `migrations/` and `internal/store/queries/`; commit the result |
| `make check-generated` | regenerate sqlc output and fail if it differs from what is committed |
| `make check-go-version` | print the Go toolchain and fail unless it is go.mod's `toolchain` |

Targets marked (P0x) do not exist yet. The phase that adds a target removes its tag.

### Local containers

- macOS uses podman machine (rootless); Linux and CI use Docker. `scripts/container.sh` picks whichever answers.
- `podman compose` needs a compose provider: `brew install docker-compose` (or podman-compose). `make dev-up` fails with a clear message when neither `podman compose` nor `docker compose` works.
- `.env.example` lists every variable. Copy it to `.env.dev` (gitignored); the Makefile loads it, its values override the shell environment, and `make VAR=value` still wins.

Web targets, `make lint` and `make test` check that `node --version` matches `.nvmrc` first; with nvm, `nvm install && nvm use` picks it up.

Go CLIs are pinned as `tool` directives in go.mod (`go get -tool`) and run with `go tool <name>`: deadcode, goose, sqlc. golangci-lint is the exception: `make tools` installs the version pinned in the Makefile into `./bin`.

## Commits

- One line: `Type: Summary`.
- Types: Feat, Fix, Refactor, Perf, Test, Doc, Style, Build, CI, Chore, Sec.
- Summary is imperative and capitalized, has no trailing period, and the whole line is at most 72 chars.
- No body. No trailers of any kind.
- One logical change per commit. Tests go in the same commit as the code they test.
- Never use `--no-verify`, never amend, never push.
- Run `make check` before pushing.
- Never pipe verification commands (make targets, tests, linters) through `tail`, `head`, `grep` or `less`: the pipeline reports the last command's status and hides the failure. Run `make check` unpiped and read its exit code.
- `.githooks/commit-msg` enforces the format (docs/adr/0003-commit-convention.md). With `COMMIT_MSG_STRICT=1` it checks a stored message without stripping anything; `scripts/check-commits.sh RANGE` runs it that way over every commit in a range.

## Code

- No dead code, no commented-out code, no unused exports, params or deps.
- No interface with a single implementation unless it is a test seam at a package boundary.
- No TODO without an issue number.
- No copy-paste: extract on second use.
- When you replace code, delete the old path in the same commit.
- Shell scripts are POSIX sh, work with GNU and BSD tools, and pass `shellcheck -s sh`.

## Frontend (`apps/web`)

- pnpm only. Every version is pinned exactly (`savePrefix: ""`); pnpm is pinned by `packageManager` and Node by `.nvmrc` and `engines` with `engineStrict`.
- Browsers: Chrome/Edge 123+, Firefox 125+, Safari 17.5+, the floor for the native popover API and CSS `light-dark()`. This is the Vite build target.
- Colours, fonts, text sizes, radii and breakpoints come only from `src/styles/tokens.css`. Tailwind's default palette and scales are cleared, so there are no raw colours and no palette classes.
- Imported documents are styled through `.reader` in `src/styles/reader.css`, not utility classes. Code never shows ligatures.
- No component library. Radix Primitives only when a component needs accessible behaviour the platform does not give (native `popover`, `<fieldset>` radios and `<details>` come first).
- Phosphor icons at 16px, imported one per file: `import { BooksIcon } from "@phosphor-icons/react/Books"`.
- Every `localStorage` access goes in try/catch and falls back to the default.
- Dev-only code sits behind `import.meta.env.DEV` so production builds drop it; `make web-build` checks the styleguide is gone.
- TypeScript is strict with `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes` and `verbatimModuleSyntax`. A Biome suppression needs a reason, like a `//nolint`.

## Tests

- Every Feat ships tests.
- Every Fix starts with a failing test.
- Go tests are table-driven and run with `-race`.
- Integration tests live behind the `integration` build tag and use testcontainers through `internal/pgtest`: `pgtest.Main` in TestMain starts one migrated Postgres per package and `DB.Reset` truncates every table before a test, so DB tests in a package run serially.
- Ryuk is off on podman, so nothing reaps containers: every container a test starts must be terminated in cleanup, failures included. `make test-clean` removes leftovers.
- Unit tests never touch the network or paid APIs. Loopback `httptest` servers are fine; DNS lookups and outside hosts are not.
- Extractor changes: `go test ./internal/extract -update` rewrites `testdata/golden/*/expected.json`; review that diff like code. `TestCodeIsVerbatim` and `TestGoldenHasNoJunk` must stay green, and run `go test -run '^$' -fuzz=FuzzExtract -fuzztime=60s ./internal/extract` before committing.
- Web: Vitest + Testing Library for units and components; Playwright (Chromium) with `@axe-core/playwright` for end-to-end and accessibility. Pages must have zero axe violations in light and dark. No screenshot assertions: they differ between macOS and Linux CI.

## Verbatim rule

Source text is never rewritten. Code blocks stay byte-identical, enforced by `TestCodeIsVerbatim` in `internal/extract` (docs/adr/0002-verbatim-source.md, docs/adr/0004-ir-and-links.md).
- A code block's text is the page's copy payload when it has one, else the `<pre>` text without line numbers, copy labels, unselectable prompts and frame chrome.
- Visible text stays. Never rewrite code with patterns; the one exception, FastAPI's terminal blocks, is in ADR 0004.

## Docs-framework adapters

- An adapter is configuration in `internal/extract/adapter`. Transforms such as callouts, tabs, code titles and line-number tables live once, in `internal/extract`. When a framework needs something new, add it there as a config field, not as adapter code.
- `Remove` is the only way an adapter drops content. Every entry gets a comment saying what chrome it is.
- Detection order is generator, then signature, then host, across all adapters; registry order breaks ties.
- Each adapter ships with:
  - at least two golden fixtures. Use a trimmed real excerpt only if the license allows it, recorded in `testdata/golden/SOURCES.md`; otherwise write synthetic HTML with the same DOM and invented text;
  - its own rules in `adapterRules` (`verbatim_test.go`), written apart from its configuration;
  - a page in `live_test.go`.
- After changing an adapter, run `make test-live`. Every page must show its adapter, code found equal to the expected count, a clean verbatim check, zero junk, and only expected warnings. If robots.txt blocks a page, report it; never work around it.

## CI

`.github/workflows/ci.yml` runs on every push to `main` and every pull request.
- Steps only call make targets. Anything CI needs becomes a make target first, so every job can be reproduced locally.
- Actions are pinned to full commit SHAs with the version in a trailing comment. When you bump one, update the SHA and the comment together; `git ls-remote https://github.com/OWNER/REPO 'refs/tags/TAG^{}'` gives the SHA.
- The workflow has `contents: read` permissions, cancels superseded runs for the same ref, and sets a timeout on every job. `make lint-go` runs actionlint on it.
- Every job runs on `ubuntu-26.04`, never `ubuntu-latest`, so the runner image only changes in a reviewed commit. `.github/actionlint.yaml` declares the label until actionlint knows it (rhysd/actionlint#682).

| Job | Runs | Reproduce locally |
|---|---|---|
| commits | `make check-commits` (the strict hook over a PR's base..head, or a push's before..sha; `sha~1..sha` after a force push or a new branch), then `make hooks-test` with dash and GNU tools | `make check-commits RANGE=main..HEAD`, `make hooks-test` |
| go | `make check-go-version`, `make tools`, `make lint-go`, `make test-go`, `make check-generated` | the same targets |
| integration | `make check-go-version`, `make test-integration` on the runner's Docker with Ryuk on | `make test-integration` |
| web | `make check-node-version`, `make web-deps`, `make lint-web`, `make test-web`, `make web-build` (styleguide guard and bundle budget) | the same targets |
| e2e, after web | `make web-browsers WITH_DEPS=1`, `make web-e2e`; uploads `apps/web/playwright-report` for 7 days when it fails | `make web-install`, `make web-e2e` |

Pull requests merge by rebase only (a repository setting). A merge commit would carry the PR title as a second line, which `check-commits` rejects.

Each job checks the toolchain it uses against its pin: Go against go.mod's `toolchain`, Node against `.nvmrc`. `make check` covers the commits, go and web jobs except `check-generated` and `web-build`. Run it before pushing.

## Done means

- `make check` (lint, test, hooks-test), run unpiped, exits 0.
- CONTEXT.md is rewritten for the new state.
- Every commit follows the format above.
- You end with a 5-line summary of what changed and what's next.
