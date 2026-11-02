# 0001: Stack

Accepted, 2026-11-02

## Context

RoNote fetches one page or reads one PDF, turns it into a structured doc, and serves an editor where students highlight, annotate and run code. The stack has to be typed and testable end to end, with as few moving parts as possible.

## Decision

- Backend in Go: `cmd/api` (HTTP) and `cmd/worker` (background jobs) on shared `internal/` packages.
- Frontend in React + TypeScript + Tailwind v4 under `apps/web/`.
- Postgres with pgvector as the only datastore.
- One monorepo, driven through the root Makefile.

## Consequences

- Two toolchains (Go and Node) live in one repo, and `make` is the single entry point to both.
- Every environment, testcontainers included, needs Postgres with the pgvector extension.
- Vector search can use the main database instead of a second store.
- When an API contract changes, the API and the web app change in the same commit.
