# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P05, CI on GitHub Actions: done locally. `.github/workflows/ci.yml` is in place and actionlint finds nothing, but it has never run, because agents don't push. The first push to github.com/ROKUMATE/RoNote will be its first run.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (docs/adr/0001-stack.md).
- Source content is verbatim and code blocks are byte-identical. Machine-made content is labelled "generated" (docs/adr/0002-verbatim-source.md).
- License: MIT. The default branch is `main`. Agents never push. Run `make check` before pushing.

Commits:
- One line, `Type: Summary`. `.githooks/commit-msg` enforces it (docs/adr/0003-commit-convention.md).
- `COMMIT_MSG_STRICT=1` runs the same hook in strict mode: it checks the stored message, strips nothing and rewrites nothing.
- `scripts/check-commits.sh RANGE` checks every commit in a range in strict mode and names each bad commit by short sha and subject.
- `scripts/commit-range.sh` picks CI's range: base..head for a PR, before..sha for a push, and `sha~1..sha` when `before` is all zeros or not an ancestor.

CI (`.github/workflows/ci.yml`; AGENTS.md has a table of jobs and how to reproduce each):
- Runs on push to `main` and on pull requests. Steps only call make targets.
- Permissions are `contents: read`. Superseded runs for the same ref are cancelled. Every job has a timeout, and checkouts use `persist-credentials: false`.
- Jobs: commits, go, integration (runner Docker, Ryuk on), web, and e2e after web.
- **Action pins (full SHAs):**
  - checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0
  - cache v6.1.0, upload-artifact v7.0.2
  - pnpm/action-setup v6.1.0
- **Caches:** golangci-lint in `bin/`, keyed on its version. Playwright browsers keyed on the Playwright version. setup-go and setup-node cache Go modules, the Go build cache and the pnpm store.
- Each job checks the toolchain it uses: go1.26.1 from go.mod's `toolchain`, Node 26.13.0 from `.nvmrc`.

Make targets:
- `check` = `lint` + `test` + `hooks-test`.
- `lint` = `lint-go` (golangci-lint, deadcode, actionlint v1.7.12 as a go tool) + `lint-web` (Biome, tsc, knip).
- `test` = `test-go` + `test-web`.
- `web-build` runs the styleguide guard and then the bundle budget in `scripts/web-budget.sh`: JS 150 kB, CSS 20 kB gzip. Today the JS is 100.8 kB and the CSS 7.8 kB.

Go side (unchanged in P05):
- Go 1.26 (`toolchain go1.26.1`). goose, sqlc and deadcode are go tools; golangci-lint v2.15.0 comes from `make tools`.
- The API needs `RONOTE_DATABASE_URL`. `/readyz` pings Postgres.
- `store` returns `ErrNotFound`/`ErrConflict`. Integration tests use `internal/pgtest` with a leak check.

Web side (unchanged in P05 apart from `.nvmrc`):
- Node 26.13.0 (`.nvmrc`, `engines`), pnpm 12.16.1, Vite 8, React 19, TypeScript 7, Tailwind 4 with the tokens in `src/styles/tokens.css`.
- Browser floor: Chrome/Edge 123+, Firefox 125+, Safari 17.5+.
- The styleguide is dev-only. Playwright runs with axe; in CI it also writes the HTML report.

## Open questions

- GNU tools for the hook scripts: closed once CI is green. The commits job runs `make hooks-test` on ubuntu with dash and GNU tools.

## Next step

P06: the verbatim guarantee (TestCodeIsVerbatim). Detailed scope not yet written.
