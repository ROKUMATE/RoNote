# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P02, Go API skeleton: done. `cmd/api` serves `/healthz` and `/readyz` with config, structured logs, middleware, a JSON error envelope and graceful shutdown. Only the standard library is used at runtime.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (docs/adr/0001-stack.md).
- Source content is verbatim and code blocks are byte-identical. Machine-made content is labelled "generated" (docs/adr/0002-verbatim-source.md).
- Commits are one line, `Type: Summary`, enforced by `.githooks/commit-msg` after `make setup`, which also sets `pull.rebase true` (docs/adr/0003-commit-convention.md). The hook is tested on macOS `/bin/sh` and dash. GNU tools: covered by CI on ubuntu-latest (P05).
- Design is typography first (DESIGN.md). Tokens arrive in P04.
- License: MIT. The default branch is `main`. Agents never push.
- `.claude/settings.json` is committed (no attribution; `--no-verify` and `git push` denied). `.claude/settings.local.json` is gitignored.

Go toolchain and lint:
- Module `github.com/rokumate/ronote`. go.mod has `go 1.26.0` and `toolchain go1.26.1`. The `go` line sits one patch lower because Go drops a toolchain line that repeats it.
- Go CLIs are go.mod `tool` directives run with `go tool`. Today that is deadcode (golang.org/x/tools v0.52.0).
- golangci-lint v2.15.0 is installed into `./bin` by `make tools`, using the official install.sh from the same tag (sha256-checked). The version lives only in `GOLANGCI_LINT_VERSION`.
- `.golangci.yml` (v2): 15 linters, gofumpt and goimports, no exclusion presets, no issue caps. `make lint` also fails if `go tool deadcode -test ./...` prints anything. Code reached only from tests counts as live.

API (`internal/config`, `internal/httpapi`, `cmd/api`):
- Config comes from env. `RONOTE_ENV` is required (dev, test, staging or prod). `RONOTE_HTTP_ADDR` defaults to `127.0.0.1:8080` and `RONOTE_LOG_LEVEL` to `info`. One error lists every bad variable.
- Logs go through slog to stdout: text in dev, JSON elsewhere.
- Middleware, outermost first: request ID, access log, panic recovery.
  - An incoming `X-Request-ID` is kept if it matches `^[A-Za-z0-9._-]{1,64}$`. Otherwise `crypto/rand.Text()` generates one.
  - The ID is echoed in the response and included in logs and error envelopes.
- Every error response, including 404 and 405, is `{"error":{"code","message","request_id"}}`, written by `writeError`. 404 and 405 come from the mux's own fallback, so `Allow` stays correct.
- Server: ReadHeaderTimeout 5s, ReadTimeout 15s, WriteTimeout 30s, IdleTimeout 60s, MaxHeaderBytes 64 KiB. SIGINT or SIGTERM triggers a shutdown with a 10s timeout.
- `/readyz` shares the `/healthz` handler until it has dependencies to check.
- Make targets still tagged (P0x) in AGENTS.md have no phase yet.

## Open questions

None right now.

## Next step

P03: Postgres + pgvector and the `/readyz` database check (detailed scope not yet written).
