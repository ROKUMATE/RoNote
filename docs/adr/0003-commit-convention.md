# 0003: Commit convention

Accepted, 2026-11-02

## Context

Coding agents write many of the commits. Left alone, they add multi-paragraph bodies and Co-authored-by, Claude-Session and "Generated with" trailers. We want a history of one line per change that can be scanned or grepped by type.

## Decision

- Every commit is one line: `Type: Summary`. The types are Feat, Fix, Refactor, Perf, Test, Doc, Style, Build, CI, Chore and Sec.
- The summary is imperative and capitalized, with no trailing period. The whole line is at most 72 chars. No body, no trailers.
- `.githooks/commit-msg` enforces the format, and `make setup` installs it through `core.hooksPath`.
- The hook drops comments, blank lines and known agent trailers, then checks the line that is left. Git's own `Merge ...` and `Revert "..."` messages pass through untouched.

## Consequences

- The reasons behind changes live in ADRs and CONTEXT.md, not in commit bodies.
- Any line containing "generated with" is dropped, so a summary that uses that phrase is rejected.
- The hook only runs on clones where `make setup` has run. A CI check can enforce the same rule later.
