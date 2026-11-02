#!/bin/sh
# Runs .githooks/commit-msg against sample messages. Exits non-zero if any
# case fails.
set -u

hook=$(cd "$(dirname "$0")/.." && pwd)/.githooks/commit-msg
work=$(mktemp -d "${TMPDIR:-/tmp}/commit-msg-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
passed=0
failed=0

# check NAME STATUS INPUT EXPECTED REASON: runs the hook on INPUT and wants
# exit STATUS, EXPECTED left in the message file and REASON (if not empty)
# somewhere in stderr.
check() {
  printf '%s\n' "$3" >"$work/msg"
  printf '%s\n' "$4" >"$work/want"
  "$hook" "$work/msg" 2>"$work/stderr"
  status=$?
  if [ "$status" -eq "$2" ] && cmp -s "$work/msg" "$work/want" &&
    { [ -z "$5" ] || grep -qF -- "$5" "$work/stderr"; }; then
    passed=$((passed + 1))
    printf 'ok    %s\n' "$1"
  else
    failed=$((failed + 1))
    printf 'FAIL  %s (exit %s, want %s)\n' "$1" "$status" "$2"
    sed 's/^/      file: /' "$work/msg"
    sed 's/^/      stderr: /' "$work/stderr"
  fi
}

# accepts NAME INPUT EXPECTED
accepts() {
  check "$1" 0 "$2" "$3" ''
}

# rejects NAME INPUT REASON: the message file must be left unchanged.
rejects() {
  check "$1" 1 "$2" "$2" "$3"
}

merge_msg="Merge branch 'import-links' into main

# Please enter a commit message to explain why this merge is necessary,
# especially if it merges an updated upstream into a topic branch."

revert_msg='Revert "Feat: Add page importer"

This reverts commit 92b0841f3c1d2e4a5b6c7d8e9f0a1b2c3d4e5f60.'

accepts 'valid' 'Feat: Add page importer' 'Feat: Add page importer'
rejects 'lowercase summary' 'Feat: add page importer' 'capital letter'
rejects 'trailing period' 'Fix: Handle empty pages.' 'period'
rejects 'two lines' 'Feat: Add page importer

Also handles printed PDFs' 'expected 1 line'
accepts 'Co-Authored-By stripped' 'Feat: Add page importer

Co-Authored-By: Claude <noreply@anthropic.com>' 'Feat: Add page importer'
accepts 'Claude-Session stripped' 'Fix: Retry slow page fetches

Claude-Session: https://claude.ai/code/session_01AbCdEf' 'Fix: Retry slow page fetches'
accepts 'emoji Generated with footer stripped' 'Doc: Add design guide

🤖 Generated with [Claude Code](https://claude.com/claude-code)

co-authored-by: Claude <noreply@anthropic.com>' 'Doc: Add design guide'
rejects '73 chars' 'Feat: Import same-domain links on click and keep other-domain links plain' \
  'line is 73 characters'
rejects 'unknown type' 'Feature: Add page importer' 'Feat Fix Refactor Perf Test Doc Style Build CI Chore Sec'
rejects 'missing space after colon' 'Feat:Add page importer' 'exactly one space'
accepts 'merge comments dropped' "$merge_msg" "Merge branch 'import-links' into main"
accepts 'revert with full hash' "$revert_msg" 'Revert "Feat: Add page importer"'
accepts '72 chars' 'Fix: Keep code blocks byte-identical when importing printed PDF handouts' \
  'Fix: Keep code blocks byte-identical when importing printed PDF handouts'
accepts 'git template comments stripped' 'Test: Cover link classification

# Please enter the commit message for your changes. Lines starting
# with '"'#'"' will be ignored, and an empty message aborts the commit.
#
# On branch main
# Changes to be committed:
#       new file:   internal/links/links_test.go' 'Test: Cover link classification'
accepts 'diff below scissors ignored' 'Refactor: Split page fetcher

# ------------------------ >8 ------------------------
# Do not modify or remove the line above.
# Everything below it will be ignored.
diff --git a/internal/fetch/fetch.go b/internal/fetch/fetch.go
+Fix: this line is part of the diff' 'Refactor: Split page fetcher'
rejects 'empty message' '# Please enter the commit message for your changes.' 'empty message'
rejects 'lowercase type' 'feat: Add page importer' 'not one of the allowed types'
accepts 'generated with in summary kept' 'Fix: Keep files generated with pandoc' \
  'Fix: Keep files generated with pandoc'
accepts 'Co-Authored-By after blank line stripped' 'Feat: Add x

Co-Authored-By: Claude <noreply@anthropic.com>' 'Feat: Add x'
accepts 'Generated with Claude Code on line 2 stripped' 'Feat: Add x
🤖 Generated with [Claude Code](https://claude.com/claude-code)' 'Feat: Add x'
accepts 'Claude-Session on line 2 stripped' 'Feat: Add x
Claude-Session: https://claude.ai/code/session_abc' 'Feat: Add x'
rejects 'Merge without git format' 'Merge stuff' 'not one of the allowed types'
rejects 'Revert without git format' 'Revert everything' 'not one of the allowed types'
accepts 'git default revert message' 'Revert "Feat: Add x"

This reverts commit 1a2b3c4.' 'Revert "Feat: Add x"'
accepts 'git merge message' "Merge branch 'p02'" "Merge branch 'p02'"
rejects 'git merge message with body' "Merge branch 'p02'

Resolve conflicts" 'expected 1 line'

printf '\n%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
