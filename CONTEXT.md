# CONTEXT.md

Current state of the project. Rewrite this file at the end of each phase; never append logs.

## Current milestone

P01.1, tighten the commit hook: done. The repo has agent rules, a design guide, ADRs, an MIT license and a tested commit message hook, and no application code yet.

## Decisions so far

- Stack: Go API and worker, React + TypeScript + Tailwind v4 web app, Postgres + pgvector, one monorepo (docs/adr/0001-stack.md).
- Source content is verbatim and code blocks are byte-identical. Machine-made content is labelled "generated" (docs/adr/0002-verbatim-source.md).
- Commits are one line, `Type: Summary`, enforced by `.githooks/commit-msg` once `make setup` has run. After line 1 the hook strips only known trailers, and only git's own merge and revert subjects skip the type check (docs/adr/0003-commit-convention.md).
- `make setup` also sets `pull.rebase true`, so pulls never create merge commits.
- The hook passes `make hooks-test` on macOS `/bin/sh` and on dash with BSD tools. GNU tools: covered by CI on ubuntu-latest (P05).
- Design is typography first: Source Serif 4, IBM Plex Sans and JetBrains Mono via Fontsource, a warm paper/ink palette with one accent, and a real dark mode (DESIGN.md). Tokens arrive in P04.
- License: MIT (LICENSE).
- The default branch is `main`. Agents never push.
- `.claude/settings.json` is committed: empty commit/PR attribution, and `git commit --no-verify` and `git push` are denied. `.claude/settings.local.json` is gitignored.
- Make targets tagged (P0x) in AGENTS.md are planned but not yet assigned to a phase.

## Open questions

None right now.

## Next step

P02 (scope not yet written).
