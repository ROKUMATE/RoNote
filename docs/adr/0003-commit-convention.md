# 0003: Commit convention

Accepted, 2026-11-02. Hook rules tightened in P01.1.

## Context

Coding agents write many of the commits. Left alone, they add multi-paragraph bodies and Co-authored-by, Claude-Session and "Generated with" trailers. We want a history of one line per change that can be scanned or grepped by type.

## Decision

- Every commit is one line: `Type: Summary`. The types are Feat, Fix, Refactor, Perf, Test, Doc, Style, Build, CI, Chore and Sec. The summary is imperative and capitalized, with no trailing period. The line is at most 72 chars. No body, no trailers.
- `.githooks/commit-msg` enforces this. `make setup` installs it through `core.hooksPath` and sets `pull.rebase true`, so pulls never create merge commits.
- The hook drops comments, blank lines and everything below git's scissors line. Line 1, the first line left, is never dropped.
- From line 2 on, the hook drops only these trailers, case-insensitive: `^co-authored-by:`, `^claude-session:`, lines containing `generated with [claude code` or `generated with claude code`, and `^this reverts commit [0-9a-f]+\.?$`.
- Exactly one line must remain, and the message file is rewritten to it. That line must match `^(Feat|Fix|Refactor|Perf|Test|Doc|Style|Build|CI|Chore|Sec): [A-Z].*[^. ]$` and be at most 72 chars.
- Only git's own formats skip the type and length checks: `^Merge (branch|branches|remote-tracking branch|pull request|tag|commit) ` (note the trailing space) and `^Revert ".+"$`.

## Consequences

- The reasons behind changes live in ADRs and CONTEXT.md, not in commit bodies.
- `git revert` works without editing. Reverting a revert (`Reapply "..."`) or a merge needs a hand-written `Type: Summary` message.
- The hook only runs on clones where `make setup` has run. CI runs the same hook in strict mode (`COMMIT_MSG_STRICT=1`, no stripping) over every pushed or proposed commit.
