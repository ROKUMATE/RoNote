# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P03, Postgres, migrations and local dev stack: done. The API needs Postgres to start, and `/readyz` reports whether the database answers. `users` is the first table, with a sqlc store behind typed errors. Integration tests run against throwaway containers on podman or Docker.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (docs/adr/0001-stack.md).
- Source content is verbatim and code blocks are byte-identical. Machine-made content is labelled "generated" (docs/adr/0002-verbatim-source.md).
- Commits are one line, `Type: Summary`, enforced by `.githooks/commit-msg` after `make setup` (which also sets `pull.rebase true`) (docs/adr/0003-commit-convention.md). GNU tools: covered by CI on ubuntu-latest (P05).
- Design is typography first (DESIGN.md). Tokens arrive in P04.
- License: MIT. The default branch is `main`. Agents never push. `.claude/settings.json` is committed.

Go toolchain and lint:
- go.mod has `go 1.26.0` and `toolchain go1.26.1`, so the pin survives `go mod tidy`.
- Go CLIs are go.mod tools run with `go tool`: deadcode, goose v3.29.0 and sqlc v1.31.1. sqlc needs cgo, so a C compiler must be present. golangci-lint v2.15.0 comes from `make tools`.
- `.golangci.yml` (v2) lints `integration` files too. `make lint` fails on any `go tool deadcode -test -tags integration ./...` output.

Runtime (`cmd/api`, `internal/config`, `internal/db`, `internal/httpapi`):
- Env config: `RONOTE_ENV` (required), `RONOTE_HTTP_ADDR` (default `127.0.0.1:8080`), `RONOTE_LOG_LEVEL` (default `info`) and `RONOTE_DATABASE_URL`.
  - The database URL is required and must be a `postgres://` URL with a host and a database name. Its value never appears in errors.
- `db.Open`: pgxpool with fixed limits (max 10 conns, 1h lifetime, 10m idle, 30s health check, 5s connect timeout). It pings with backoff (100ms doubling to 2s) and the API exits after 30s if the database never answers.
- `/healthz` has no dependencies. `/readyz` pings through the `Pinger` seam with a 1s timeout and answers 503 `not_ready` when the ping fails.
- Logs: slog to stdout, text in dev and JSON elsewhere. Durations are `duration_ms` floats with 3 decimals.
- Errors use one envelope, `{"error":{"code","message","request_id"}}`. Request IDs come from `X-Request-ID` when valid, otherwise `crypto/rand.Text()`.

Data (`migrations/`, `internal/store`):
- goose SQL migrations, numbered sequentially. `00001_users.sql` uses the built-in `uuidv7()` and a unique index on `lower(email)`. No extensions yet.
- sqlc (pgx/v5) generates `internal/store/sqlcdb`, which is committed. `uuid` maps to google/uuid and `timestamptz` to `time.Time`.
- `store` returns `ErrNotFound` and `ErrConflict` (SQLSTATE 23505). Other errors keep their message but drop the pgx error chain.

Dev stack and tests:
- `deploy/compose.dev.yml` runs `pgvector/pgvector:0.8.7-pg18-trixie`, pinned by index digest (arm64 and amd64, Postgres 18.6). It listens on `127.0.0.1:5432`, and its volume is mounted at `/var/lib/postgresql` (where the PG18 images keep data).
- `scripts/container.sh` picks podman machine or Docker for compose, integration tests and cleanup. `.env.dev`, created from `.env.example`, is loaded by the Makefile.
- `internal/pgtest`:
  - `Main` gives each package one migrated container. `New(t)` gives a test its own container.
  - `Reset` truncates every table, so DB tests in a package run serially. Schema-per-test would break once the pgvector extension types arrive.
- Ryuk is off on podman. Containers carry the label `ronote.pgtest=true`, and `make test-integration` fails if any survive. `make test-clean` removes them.

## Open questions

None right now.

## Next step

P04: design tokens (detailed scope not yet written).
