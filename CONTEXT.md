# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P01, repo conventions and guardrails: done. The repo has agent rules, a design guide, ADRs and a commit message hook, and no application code yet.

## Decisions so far

- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (docs/adr/0001-stack.md).
- Source content is verbatim and code blocks are byte-identical. Machine-made content is labelled "generated" (docs/adr/0002-verbatim-source.md).
- Commits are one line, `Type: Summary`, enforced by `.githooks/commit-msg` once `make setup` has run (docs/adr/0003-commit-convention.md).
- Design is typography first: Source Serif 4, IBM Plex Sans and JetBrains Mono via Fontsource, a warm paper/ink palette with one accent, and a real dark mode (DESIGN.md). Tokens arrive in P04.
- The default branch is `main`. Agents never push.
- `.claude/settings.json` is committed: empty commit/PR attribution, and `git commit --no-verify` and `git push` are denied.
- Make targets tagged (P0x) in AGENTS.md are planned but not yet assigned to a phase.

## Open questions

- License: not chosen yet, so there is no LICENSE file.
- The hook has passed `make hooks-test` on macOS `/bin/sh` and on dash with BSD tools. It has not yet been run with GNU tools (Debian container).

## Next step

P02 (scope not yet written).
