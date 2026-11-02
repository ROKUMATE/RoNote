# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P04, web skeleton, design tokens and fonts: done.
- `apps/web` is a Vite + React + TypeScript app with the design tokens, self-hosted fonts and an app shell.
- It has a Library page, theme and reader-font preferences, and a dev-only styleguide.
- It makes no backend calls yet; the API client comes in P08.

## Decisions so far

Product and process:
- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (docs/adr/0001-stack.md).
- Source content is verbatim and code blocks are byte-identical. Machine-made content is labelled "generated" (docs/adr/0002-verbatim-source.md).
- Commits are one line, `Type: Summary`, enforced by `.githooks/commit-msg` after `make setup` (docs/adr/0003-commit-convention.md).
- License: MIT. The default branch is `main`. Agents never push.

Go side (unchanged in P04):
- Go 1.26 (`toolchain go1.26.1`). Tools are deadcode, goose and sqlc via `go tool`, plus golangci-lint v2.15.0 via `make tools`.
- The API needs `RONOTE_DATABASE_URL`. `/readyz` pings Postgres (1s timeout, 503 `not_ready`).
- `store` returns `ErrNotFound`/`ErrConflict`.
- Integration tests use `internal/pgtest` with testcontainers, and a leak check runs after them.
- Dev stack: `pgvector/pgvector:0.8.7-pg18-trixie` (digest-pinned) via `scripts/container.sh` (podman machine or Docker).

Web toolchain (`apps/web`):
- **Runtimes:** Node 26.13.0 (`.node-version`, `engines`, `engineStrict`) and pnpm 12.16.1 (`packageManager`). Every dependency is pinned exactly. `make` refuses to run web targets on another Node version.
- **App libraries:** Vite 8.3.5, React 19.3.1, TypeScript 7.0.2 (the native compiler), Tailwind 4.3.3, TanStack Router 1.170.46 (code-based routes), Phosphor 2.1.10.
- **Lint and test tools:** Biome 2.6.4 (`--error-on-warnings`), knip 6.44.1, Vitest 5.0.5 + jsdom 30 + Testing Library, Playwright 1.64.1 (Chromium) + @axe-core/playwright 4.14.0.
- **Browser floor:** Chrome/Edge 123+, Firefox 125+, Safari 17.5+, because of the native popover API and CSS `light-dark()`.
  - This is also the Vite build target. Tailwind's own optimizer is off, because it would rewrite `light-dark()` into a fallback that breaks per-element theming.
- **Tokens:** `src/styles/tokens.css` uses `@theme static` with Tailwind's defaults cleared.
  - Each colour is one `light-dark()` value; `color-scheme` follows the system and `data-theme` on `<html>` overrides it.
  - Fonts: Source Serif 4, IBM Plex Sans and JetBrains Mono as Fontsource variable packages (`wght`, plus italics for Serif and Plex).
  - Code never shows ligatures.
- **Reader typography** lives in `.reader` (`src/styles/reader.css`), because imported documents arrive as HTML.
- **Preferences:** `src/preferences.ts` (theme system/light/dark, reader font serif/sans), with every storage call guarded.
  - An inline script in `index.html` applies the saved values before first paint. A Vitest test runs that script, so the two can't drift.
- **Settings:** a native `popover` in the sidebar footer. The disabled "Import page" button uses a small Tooltip (hover, focus, Escape).
- **Code blocks:** `CodeBlock` is focusable (`tabIndex=0`), because axe requires scrollable regions to be reachable by keyboard. Biome's suppression for that rule carries the reason.
- **Styleguide:** `/_styleguide` exists only in dev (`import.meta.env.DEV`). `make web-build` fails if any styleguide code reaches `dist/`.
- **Production bundle:** about 101 KB gzip JS and 8 KB gzip CSS. Fonts are separate and fetched per unicode-range subset.

## Open questions

None right now.

## Next step

P05: CI on ubuntu-latest (Go, web, integration and e2e), detailed scope not yet written.
